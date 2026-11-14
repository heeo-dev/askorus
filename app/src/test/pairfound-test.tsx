// Pairing without a QR code (#58). The phone and the PC are signed in to the
// same Tailscale account, so the app finds the PC among the account's devices
// and asks; the person allows it on the PC (an open pairing window, a
// question on its screen) or on a phone that is already paired. Two digits
// shown on both sides tell the request apart from another device's.

import { userEvent } from '@testing-library/react-native';
import { act, renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import Pair from '@/app/pair';
import { AgentClient, baseUrl } from '@/api/client';
import { findPCs, newMatch } from '@/connections/discover';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';
import { mockTailnet, neverSignedIn } from '@/test/tailnet';
import { ownAsk, pairAsk } from '@/ui/PairRequests';

let agent: FakeAgent;

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
  pair: Pair,
};

// The account's devices: the PC's agent, another phone with the app, and a
// device that has nothing to do with Askorus.
const account = [
  { name: HOST, host: 'askorus-desktop-test', online: true, os: 'windows' },
  { name: 'askorus-other-phone.tail0.ts.net', host: 'askorus-other-phone', online: true, os: 'android' },
  { name: 'nas.tail0.ts.net', host: 'nas', online: true, os: 'linux' },
  { name: 'askorus-asleep.tail0.ts.net', host: 'askorus-asleep', online: false, os: 'windows' },
];

beforeEach(() => {
  agent = new FakeAgent();
  agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' })];
  globalThis.fetch = agent.fetch;
  pairAsk.everyMs = 5000;
});

afterEach(() => jest.restoreAllMocks());

test('the PCs of the account are found by their name and their answer', async () => {
  mockTailnet.devices = account;
  const found = await findPCs([]);
  expect(found).toEqual([{ host: HOST, port: PORT, name: 'DESKTOP-TEST', os: 'windows', window: false, paired: false }]);
  // Only devices named as the agent names its own are asked at all.
  // (the PC and the other phone with the app; not the NAS, not the PC that is off).
  expect(agent.calls.map((c) => `${c.method} ${c.path}`)).toEqual(['GET /v1/hello', 'GET /v1/hello']);
  expect(agent.calls.every((c) => c.auth === undefined)).toBe(true); // no token: the phone is not paired yet

  agent.hello = 'window';
  const paired = await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  expect(await findPCs([paired])).toEqual([expect.objectContaining({ window: true, paired: true })]);
  // An agent that does not answer the hello is not listed.
  agent.hello = null;
  expect(await findPCs([])).toEqual([]);
});

test('the two digits are two digits', () => {
  expect(newMatch(() => 0)).toBe('10');
  expect(newMatch(() => 0.999)).toBe('99');
  for (let i = 0; i < 50; i++) expect(newMatch()).toMatch(/^[1-9]\d$/);
});

test('a PC found in the account is paired by a tap: no QR code, and the two digits show while the PC is asked', async () => {
  mockTailnet.devices = account;
  agent.pairAnswer = 'wait';
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pair' });

  const list = within(await screen.findByTestId('found-pcs'));
  expect(list.getByText('DESKTOP-TEST')).toBeTruthy();
  expect(list.getByText('Windows · desktop-test')).toBeTruthy();
  expect(list.queryByText(/other-phone|nas|asleep/)).toBeNull();
  // The QR code steps fold away while a PC is listed.
  expect(screen.queryByRole('button', { name: 'Scan QR code' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Pair with a QR code or a link' })).toBeTruthy();

  await user.press(list.getByRole('button', { name: 'DESKTOP-TEST' }));
  expect(await screen.findByText('Is this your PC?')).toBeTruthy();
  expect(screen.getByText('This PC was found in your own Tailscale account.')).toBeTruthy();
  expect(screen.queryByTestId('pair-warn')).toBeNull(); // the warning is about links and QR codes someone sent

  await user.press(screen.getByRole('button', { name: 'Pair' }));
  const digits = await screen.findByTestId('pair-match');
  await waitFor(() => expect(agent.lastMatch).toMatch(/^[1-9]\d$/));
  expect(digits).toHaveTextContent(agent.lastMatch);
  expect(screen.getByText(/Allow it if the PC shows the same number/)).toBeTruthy();
  const sent = agent.calls.find((c) => c.method === 'POST' && c.path === '/v1/pair');
  expect(sent?.body).toEqual({ deviceName: expect.any(String), match: agent.lastMatch });

  agent.pairAnswer = 'approve'; // allowed on the PC
  // The number stays on the screen: a pairing window on the PC takes the
  // phone at once, and this is where it can be compared with what the PC said.
  expect(await screen.findByText('Paired')).toBeTruthy();
  expect(screen.getByTestId('pair-match')).toHaveTextContent(agent.lastMatch);
  expect(screen.getByText(/A different number there means another device came in instead/)).toBeTruthy();
  const [pc] = await new ConnectionStore().list();
  expect(pc).toMatchObject({ host: HOST, port: PORT, name: 'DESKTOP-TEST' });
  await user.press(screen.getByRole('button', { name: 'Done' }));
  expect(await screen.findByText('Fix the login bug')).toBeTruthy();
});

test('the QR code is still there for a PC that is not listed', async () => {
  mockTailnet.devices = account;
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pair' });
  await screen.findByTestId('found-pcs');
  await user.press(screen.getByRole('button', { name: 'Pair with a QR code or a link' }));
  expect(screen.getByText('If your PC is not listed, use the QR code')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Scan QR code' })).toBeEnabled();
  expect(screen.getByText('askorus-agent pair')).toBeTruthy();
});

test('no PC in the account yet: it says what to look at, and the QR code steps stay open', async () => {
  await renderRouter(routes, { initialUrl: '/pair' });
  expect(await screen.findByText(/No PC found yet/)).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Scan QR code' })).toBeEnabled();
  expect(screen.queryByTestId('found-pcs')).toBeNull();
});

test('not signed in: nothing is looked for', async () => {
  mockTailnet.devices = account;
  await neverSignedIn();
  await renderRouter(routes, { initialUrl: '/pair' });
  expect(await screen.findByText('Once you are signed in, the PCs found in your Tailscale account show up here.')).toBeTruthy();
  expect(agent.calls).toHaveLength(0);
});

test('nobody on the PC could allow it: the phone says to open a pairing window there', async () => {
  mockTailnet.devices = account;
  agent.canApprove = false;
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pair' });
  await user.press(within(await screen.findByTestId('found-pcs')).getByRole('button', { name: 'DESKTOP-TEST' }));
  await user.press(await screen.findByRole('button', { name: 'Pair' }));
  expect(await screen.findByText(/Run askorus-agent pair on the PC, then tap this PC again/)).toBeTruthy();
});

// The other side: this phone is paired already, and another phone asks.
describe('a paired phone answers', () => {
  const question = { id: 'q1', deviceName: 'New phone', match: '47', at: '2026-11-14T00:00:00Z' };

  beforeEach(async () => {
    await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
    pairAsk.everyMs = 30;
  });

  test('the request shows with the name and the two digits, and Allow answers it', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    expect(screen.queryByTestId('pair-ask')).toBeNull();

    agent.pairQuestions = [question];
    const ask = within(await screen.findByTestId('pair-ask'));
    expect(ask.getByText('Another phone asked to pair with DESKTOP-TEST.')).toBeTruthy();
    expect(ask.getByText('New phone')).toBeTruthy();
    expect(ask.getByText('47')).toBeTruthy();
    expect(ask.getByText(/only if you just asked for this on that phone and it shows the same number/)).toBeTruthy();
    await user.press(ask.getByRole('button', { name: 'Allow' }));
    await waitFor(() => expect(agent.pairAnswers).toEqual([{ id: 'q1', allow: true }]));
    await waitFor(() => expect(screen.queryByTestId('pair-ask')).toBeNull());
  });

  test('Refuse answers no, and a question that was closed does not come back', async () => {
    const user = userEvent.setup();
    agent.pairQuestions = [question];
    await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(within(await screen.findByTestId('pair-ask')).getByRole('button', { name: 'Refuse' }));
    await waitFor(() => expect(agent.pairAnswers).toEqual([{ id: 'q1', allow: false }]));

    // Closed without an answer: the PC still lists it, this phone does not ask again.
    agent.pairAnswers = [];
    agent.pairQuestions = [{ ...question, id: 'q2', deviceName: 'Third phone' }];
    await user.press(within(await screen.findByTestId('pair-ask')).getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByTestId('pair-ask')).toBeNull());
    await act(async () => {
      jest.advanceTimersByTime(300); // ten more looks
    });
    expect(screen.queryByTestId('pair-ask')).toBeNull();
    expect(agent.pairAnswers).toEqual([]);
  });

  // Found on a real phone: allowed on the PC's screen, the question stayed on
  // the paired phone with buttons that did nothing.
  test('a question that was answered elsewhere goes away by itself', async () => {
    agent.pairQuestions = [question];
    await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByTestId('pair-ask')).toBeTruthy();
    agent.pairQuestions = []; // answered on the PC, or its time is up
    await waitFor(() => expect(screen.queryByTestId('pair-ask')).toBeNull());
    expect(agent.pairAnswers).toEqual([]);
    // The next phone's question shows as usual.
    agent.pairQuestions = [{ ...question, id: 'q2', deviceName: 'Third phone' }];
    expect(within(await screen.findByTestId('pair-ask')).getByText('Third phone')).toBeTruthy();
  });

  test('a phone is not asked about its own request', async () => {
    ownAsk.name = 'New phone';
    ownAsk.match = '47';
    try {
      agent.pairQuestions = [question];
      await renderRouter(routes, { initialUrl: '/pcs' });
      await screen.findByText('Fix the login bug');
      await act(async () => {
        jest.advanceTimersByTime(300); // ten looks
      });
      expect(screen.queryByTestId('pair-ask')).toBeNull();
      // Another phone that happens to have the same name shows another number.
      agent.pairQuestions = [question, { ...question, id: 'q2', match: '48' }];
      expect(within(await screen.findByTestId('pair-ask')).getByText('48')).toBeTruthy();
    } finally {
      ownAsk.name = '';
      ownAsk.match = '';
    }
  });

  // R-115, found on a real phone: disconnected on the PC, the phone went on
  // asking every five seconds with its dead token, the agent counted ten
  // failed attempts within a minute, and the phone could not pair again.
  test('a phone that was disconnected on the PC stops asking', async () => {
    agent.tokenValid = false;
    await renderRouter(routes, { initialUrl: '/pcs' });
    // One request finds out that the token is dead. Nothing follows it:
    // not the waiting pairing requests, not the screens that refresh.
    const sent = () => agent.calls.filter((c) => c.auth).length + agent.pairPolls;
    await waitFor(() => expect(sent()).toBe(1));
    await act(async () => {
      jest.advanceTimersByTime(300); // ten more looks
    });
    expect(sent()).toBe(1);
    // Paired again (a new device id on the same PC): it asks again.
    agent.tokenValid = true;
    const store = new ConnectionStore();
    const [old] = await store.list();
    await store.add({ kind: 'pc', name: old.name, host: old.host, port: old.port, deviceId: 'dev-2', deviceName: old.deviceName, pairedAt: 'y' }, TOKEN);
    await waitFor(() => expect(agent.pairPolls).toBeGreaterThan(0));
  });

  test('an agent from before this has no list: it is asked once and left alone', async () => {
    const real = agent.fetch;
    let asks = 0;
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith('/v1/pair/requests')) {
        asks++;
        return new Response(JSON.stringify({ error: { code: 'not_found' } }), { status: 404 });
      }
      return real(input, init);
    }) as typeof fetch;
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    await waitFor(() => expect(asks).toBe(1));
    await act(async () => {
      jest.advanceTimersByTime(300); // ten more looks
    });
    expect(asks).toBe(1);
  });
});

test('the client asks without a code and reads the waiting requests', async () => {
  const client = new AgentClient(baseUrl(HOST, PORT));
  expect((await client.hello()).hostname).toBe('DESKTOP-TEST');
  agent.pairQuestions = [{ id: 'q1', deviceName: 'New phone', match: '47', at: 'x' }];
  const withToken = client.withToken(TOKEN);
  expect(await withToken.pairRequests()).toEqual(agent.pairQuestions);
  await withToken.answerPair('q1', true);
  expect(agent.pairAnswers).toEqual([{ id: 'q1', allow: true }]);
  await expect(client.pairRequests()).rejects.toMatchObject({ status: 401 });
});
