// The app's own Tailscale node (#57), against the tests' stand-in for the
// native module (test/setup.ts).

import * as SecureStore from 'expo-secure-store';

import { AgentClient, baseUrl, NetworkError } from '@/api/client';
import { cause } from '@/connections/reach';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { finishSignIn, LOGIN_URL, mockTailnet, neverSignedIn } from '@/test/tailnet';

import { loginTailnet, logoutTailnet, ownDevice, readTailnet, resetTailnet, resumeTailnet, TailnetError, tailnetFetch, tailnetHostName } from './node';

let agent: FakeAgent;

beforeEach(() => {
  agent = new FakeAgent();
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

const native = () => jest.requireMock('@/tailnet/native').nativeTailnet();

test("the phone's name in the tailnet is made of what a DNS label may hold", () => {
  expect(tailnetHostName('Pixel 9 Pro')).toBe('askorus-pixel-9-pro');
  expect(tailnetHostName("Sam's Phone")).toBe('askorus-sams-phone');
  expect(tailnetHostName('  --SM_TEST1..  ')).toBe('askorus-sm-test1');
  expect(tailnetHostName('내 폰')).toBe('askorus-phone');
  expect(tailnetHostName('')).toBe('askorus-phone');
  expect(tailnetHostName(null)).toBe('askorus-phone');
  const long = tailnetHostName('a'.repeat(50) + '-' + 'b'.repeat(50));
  expect(long.length).toBeLessThanOrEqual(63);
  expect(long.endsWith('-')).toBe(false);
});

// Until the person chooses to sign in, the app starts no node and so talks
// to no server at all, Tailscale's included.
test('a phone that never signed in starts nothing and sends nothing', async () => {
  await neverSignedIn();
  await resumeTailnet();
  expect((await readTailnet()).state).toBe('off');
  await expect(tailnetFetch(`${baseUrl(HOST, PORT)}/v1/info`)).rejects.toMatchObject({ code: 'tailnet_login' });
  await expect(ownDevice(HOST)).rejects.toMatchObject({ code: 'tailnet_login' });
  expect(mockTailnet.calls).toEqual([]);
  expect(agent.calls).toHaveLength(0);

  // A client's request fails as a network error that knows why, so the
  // screens can offer the sign-in instead of guessing.
  const e = await new AgentClient(baseUrl(HOST, PORT), { token: TOKEN }).info().catch((x) => x);
  expect(e).toBeInstanceOf(NetworkError);
  expect(e.cause).toBeInstanceOf(TailnetError);
  expect(cause(e)).toBe('tailnet_login');
});

test('signing in starts the node, brings the link, and is remembered for the next launch', async () => {
  await neverSignedIn();
  await loginTailnet();
  expect(mockTailnet.calls).toEqual(['start askorus-test-phone', 'login']);
  expect(await readTailnet()).toEqual({ state: 'needsLogin', loginURL: LOGIN_URL });
  expect(await SecureStore.getItemAsync('tailnet.on.v1')).toBe('1');

  finishSignIn();
  expect(await readTailnet()).toEqual({ state: 'running', name: 'askorus-test-phone.tail0.ts.net', owner: 'me@example.com' });

  // The next launch: the node starts by itself, with no new sign-in.
  mockTailnet.calls = [];
  resetTailnet();
  await resumeTailnet();
  expect(mockTailnet.calls).toEqual(['start askorus-test-phone']);
  expect((await readTailnet()).state).toBe('running');
});

test('a sign-in that ran out shows as needing a sign-in, and the button asks for a new link', async () => {
  mockTailnet.status = { state: 'NeedsLogin' }; // no link until it is asked for
  mockTailnet.afterLogin = { state: 'NeedsLogin', authURL: LOGIN_URL };
  expect(await readTailnet()).toEqual({ state: 'needsLogin' });
  await loginTailnet();
  expect(mockTailnet.calls).toContain('login');
  expect(await readTailnet()).toEqual({ state: 'needsLogin', loginURL: LOGIN_URL });
});

test('the other states of the node', async () => {
  mockTailnet.status = { state: 'NeedsMachineAuth', name: 'askorus-test-phone.tail0.ts.net' };
  expect((await readTailnet()).state).toBe('needsApproval');
  mockTailnet.status = { state: 'Starting' };
  expect((await readTailnet()).state).toBe('starting');
  mockTailnet.status = { state: 'Running', keyExpiry: '2027-05-01T00:00:00Z' };
  expect(await readTailnet()).toEqual({ state: 'running', keyExpiry: '2027-05-01T00:00:00Z' });
  mockTailnet.available = false;
  resetTailnet();
  expect((await readTailnet()).state).toBe('unavailable');
  await expect(tailnetFetch('http://x.ts.net:1/')).rejects.toMatchObject({ code: 'tailnet_off' });
  expect(await ownDevice(HOST)).toBe('unavailable');
});

test('a request goes through the node as it is, and its answer comes back', async () => {
  const fetch = jest.spyOn(native(), 'fetch');
  const info = await new AgentClient(baseUrl(HOST, PORT), { token: TOKEN }).info();
  expect(info.version).toBe('0.1.0');
  expect(fetch).toHaveBeenCalledWith('GET', `http://${HOST}:${PORT}/v1/info`, JSON.stringify({ Accept: 'application/json', Authorization: `Bearer ${TOKEN}` }), '', expect.any(Number));
  expect(agent.calls[0]).toMatchObject({ method: 'GET', path: '/v1/info', auth: `Bearer ${TOKEN}` });

  // A body is sent as text; an error status is the agent's error.
  fetch.mockResolvedValueOnce({ status: 403, headers: '{"Content-Type":"application/json"}', body: '{"error":{"code":"wrong_account","message":"x"}}' });
  const e = await new AgentClient(baseUrl(HOST, PORT), { token: TOKEN }).startRun({ mode: 'ask', prompt: 'hi', agents: ['claude'] } as never).catch((x) => x);
  expect(e).toMatchObject({ name: 'AgentError', code: 'wrong_account', status: 403 });
  expect(JSON.parse(fetch.mock.calls[1][3] as string)).toMatchObject({ prompt: 'hi' });

  const res = await tailnetFetch(`http://${HOST}:${PORT}/v1/info`, { headers: { Authorization: `Bearer ${TOKEN}` } });
  expect(res.ok).toBe(true);
  expect(res.headers.get('content-type')).toBeNull();
  expect(JSON.parse(await res.text()).version).toBe('0.1.0');
});

// The native side sends only to a device of the person's own Tailscale
// account, and says so with a code when it refuses.
test("the node's refusals reach the screens as their cause", async () => {
  const fetch = jest.spyOn(native(), 'fetch');
  const client = new AgentClient(baseUrl(HOST, PORT), { token: TOKEN });
  const refusal = (code: string) => Object.assign(new Error('refused'), { code });

  fetch.mockRejectedValueOnce(refusal('tailnet_not_own'));
  expect(cause(await client.info().catch((x) => x))).toBe('not_own_pc');
  fetch.mockRejectedValueOnce(refusal('tailnet_unknown_host'));
  expect(cause(await client.info().catch((x) => x))).toBe('not_own_pc');
  fetch.mockRejectedValueOnce(refusal('tailnet_login'));
  expect(cause(await client.info().catch((x) => x))).toBe('tailnet_login');
  // Anything else is the network's: the phone's side when it fails at once.
  fetch.mockRejectedValueOnce(new Error('dial tcp: i/o timeout'));
  const e = await client.info().catch((x) => x);
  expect(e.cause).toMatchObject({ code: 'tailnet_network' });
  expect(cause(e)).toBe('phone_offline');
});

test('a request the client gave up on ends at once', async () => {
  jest.spyOn(native(), 'fetch').mockImplementation(() => new Promise(() => undefined)); // never answers
  const ctl = new AbortController();
  const pending = tailnetFetch(`http://${HOST}:${PORT}/v1/info`, { signal: ctl.signal });
  ctl.abort();
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  // The client's own timeout reads as a PC that does not answer.
  const e = await new AgentClient(baseUrl(HOST, PORT), { token: TOKEN, timeoutMs: 20 }).info().catch((x) => x);
  expect(e).toBeInstanceOf(NetworkError);
  expect(e.timedOut).toBe(true);
  expect(cause(e)).toBe('pc_unreachable');
});

test('whose device a PC name is', async () => {
  expect(await ownDevice(HOST)).toBe('own');
  mockTailnet.peers[HOST] = { known: true, sameUser: false };
  expect(await ownDevice(HOST)).toBe('other');
  mockTailnet.peers[HOST] = { known: false, sameUser: false };
  expect(await ownDevice(HOST)).toBe('unknown');
});

test('signing out removes the phone from the tailnet and stops the node; the next launch starts nothing', async () => {
  await readTailnet(); // the node is up
  mockTailnet.calls = [];
  await logoutTailnet();
  expect(mockTailnet.calls).toEqual(['logout', 'stop']);
  expect(await SecureStore.getItemAsync('tailnet.on.v1')).toBeNull();
  expect((await readTailnet()).state).toBe('off');
  await expect(tailnetFetch(`http://${HOST}:${PORT}/v1/info`)).rejects.toMatchObject({ code: 'tailnet_login' });

  mockTailnet.calls = [];
  resetTailnet();
  await resumeTailnet();
  expect(mockTailnet.calls).toEqual([]);
});
