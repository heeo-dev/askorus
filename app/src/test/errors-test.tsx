// B1-5 on the S5c screens: errors explained by cause on 내 PC, several PCs
// kept apart (F7), and what the PC reports.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import Pair from '@/app/pair';
import { loadAskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

let agent: FakeAgent;

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
  pair: Pair,
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' })];
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

async function pairedPC(name = 'DESKTOP-TEST', host = HOST, token = TOKEN) {
  return new ConnectionStore().add({ kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, token);
}

test('agent stopped behind Tailscale serve: says so and how to start it', async () => {
  agent.reach = 'agent_down';
  await pairedPC();
  await renderRouter(routes, { initialUrl: '/pcs' });
  // The PC's line and the box both name the cause.
  expect(await screen.findAllByText('The PC is on, but the agent is not running')).toHaveLength(2);
  expect(screen.getByText(/askorus-agent start/)).toBeTruthy();
  expect(screen.getAllByTestId('dot-failed', { includeHiddenElements: true }).length).toBeGreaterThanOrEqual(1);
  // A PC that does not answer offers no new session, which could only fail (UX-4).
  expect(screen.queryByRole('button', { name: 'New session' })).toBeNull();
});

test('cannot reach the PC: lists what to check, phone first, and retry works', async () => {
  agent.reach = 'offline';
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(routes, { initialUrl: '/pcs' });
  expect(await screen.findAllByText("Can't reach the PC")).toHaveLength(2);
  expect(screen.getByText(/^1\. Check that Tailscale is on and connected on this phone/)).toBeTruthy();
  expect(screen.getByText(/^2\. Check that the PC is on and awake/)).toBeTruthy();
  // The shell learns it too: no waiting dot from a PC that does not answer.
  expect(screen.getByRole('tab', { name: 'My PCs' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'New session' })).toBeNull(); // UX-4

  agent.reach = 'ok';
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('Fix the login bug')).toBeTruthy();
  await waitFor(() => expect(screen.queryByText(/^1\. Check that Tailscale/)).toBeNull());
  expect(await screen.findByText('Connected · agent 0.1.0')).toBeTruthy();
  // Once it answers, a new session can be started again.
  expect(screen.getByRole('button', { name: 'New session' })).toBeTruthy();
});

test('a PC that dropped this phone offers pairing again', async () => {
  agent.tokenValid = false;
  const user = userEvent.setup();
  await pairedPC();
  const r = await renderRouter(routes, { initialUrl: '/pcs' });
  expect(await screen.findAllByText('This phone is no longer paired with the PC')).toHaveLength(2);
  // One apricot action: 'Pair again', not also 'New session' (UX-4).
  expect(screen.queryByRole('button', { name: 'New session' })).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Pair again' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pair'));
});

test('what the PC reports about Claude shows above the list', async () => {
  agent.notices = ['claude_remote_control_disabled_env'];
  await pairedPC();
  await renderRouter(routes, { initialUrl: '/pcs' });
  expect(await screen.findByText('An environment variable on the PC turns Remote Control off.')).toBeTruthy();
});

test('a missing AI that only asks says nothing above the session list (P2-1)', async () => {
  agent.askAgents = [{ agent: 'opencode', available: false, capabilities: [], notices: ['opencode_not_found'] }];
  agent.sessions = [session({ id: 'claude:5', title: 'Asked about HTTPS', state: 'history', badges: ['ask'] })];
  await pairedPC();
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pcs' });
  await user.press(await screen.findByRole('button', { name: 'Ask chats' })); // asks are left out of the other chips (P2-2)
  expect(await screen.findByText('Asked about HTTPS')).toBeTruthy();
  expect(screen.queryByText(/opencode_not_found/)).toBeNull();
});

test('a PC that cannot read which sessions run says states may be unknown', async () => {
  const fetch0 = agent.fetch;
  globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const res = await fetch0(u, init);
    if (new URL(String(u)).pathname !== '/v1/sessions') return res;
    const body = await res.json();
    return new Response(JSON.stringify({ ...body, warnings: ['claude_agents_unavailable'] }), { status: 200, headers: { 'Content-Type': 'application/json' } });
  }) as typeof fetch;
  await pairedPC();
  await renderRouter(routes, { initialUrl: '/pcs' });
  expect(await screen.findByText(/states may show as Unknown/)).toBeTruthy();
});

test('two PCs stay apart: the picker shows each with its own state, and picking one shows its sessions (F7)', async () => {
  const other = new FakeAgent();
  other.sessions = [session({ id: 'claude:9', title: 'Laptop work', state: 'stopped' })];
  const laptopHost = 'laptop.tail0.ts.net';
  globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(u));
    if (url.hostname === laptopHost) {
      // The laptop's agent only knows its own token.
      const auth = (init?.headers as Record<string, string>).Authorization;
      if (auth !== 'Bearer laptop-token') return new Response('{"error":{"code":"unauthorized"}}', { status: 401 });
      return other.fetch(`https://${HOST}:${PORT}${url.pathname}${url.search}`, {
        ...init,
        headers: { ...(init?.headers as Record<string, string>), Authorization: `Bearer ${TOKEN}` },
      });
    }
    return agent.fetch(u, init);
  }) as typeof fetch;
  await pairedPC();
  const laptop = await pairedPC('LAPTOP', laptopHost, 'laptop-token');

  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pcs' });
  // The first paired PC is the current one until another is picked.
  expect(await screen.findByText('Fix the login bug')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Pick another PC' }));
  const picker = within(await screen.findByTestId('pc-picker'));
  expect(await picker.findByRole('button', { name: `DESKTOP-TEST, Windows · ${HOST}, Connected · agent 0.1.0` })).toBeSelected();
  expect(await picker.findByRole('button', { name: `LAPTOP, Windows · ${laptopHost}, Connected · agent 0.1.0` })).not.toBeSelected();

  await user.press(picker.getByRole('button', { name: `LAPTOP, Windows · ${laptopHost}, Connected · agent 0.1.0` }));
  expect(await screen.findByText('Laptop work')).toBeTruthy();
  expect(screen.queryByText('Fix the login bug')).toBeNull();
  expect(screen.getByRole('header', { name: 'LAPTOP' })).toBeTruthy();
  // The pick is kept: the laptop now also answers the questions.
  await waitFor(async () => expect((await loadAskPrefs()).pc).toBe(laptop.id));
  expect(other.callsTo('GET', '/v1/sessions').length).toBeGreaterThanOrEqual(1);
});

test('the picker names a PC’s cause in one line and leads to pairing', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await pairedPC('OFFICE', 'office.tail0.ts.net'); // the fake answers only HOST: this one is offline
  const r = await renderRouter(routes, { initialUrl: '/pcs' });
  await user.press(await screen.findByRole('button', { name: 'Pick another PC' }));
  const picker = within(await screen.findByTestId('pc-picker'));
  expect(await picker.findByRole('button', { name: "OFFICE, office.tail0.ts.net, Can't reach the PC" })).toBeTruthy();
  await user.press(picker.getByRole('button', { name: 'Pair with a PC' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pair'));
});
