// B1-5: errors explained by cause, several PCs, what the PC reports, and
// the hidden second entrance.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';

beforeEach(() => {
  agent = new FakeAgent();
  agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' })];
  globalThis.fetch = agent.fetch;
});

async function pairedPC(name = 'DESKTOP-TEST', host = HOST, token = TOKEN) {
  return new ConnectionStore().add(
    { kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' },
    token,
  );
}

test('agent stopped behind Tailscale serve: says so and how to start it', async () => {
  agent.reach = 'agent_down';
  const c = await pairedPC();
  await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
  expect(await screen.findByText('The PC is on, but the agent is not running')).toBeTruthy();
  expect(screen.getByText(/asksemble-agent start/)).toBeTruthy();
});

test('cannot reach the PC: lists what to check, phone first, and retry works', async () => {
  agent.reach = 'offline';
  const user = userEvent.setup();
  const c = await pairedPC();
  await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
  expect(await screen.findByText("Can't reach the PC")).toBeTruthy();
  expect(screen.getByText(/^1\. Check that Tailscale is on and connected on this phone/)).toBeTruthy();
  expect(screen.getByText(/^2\. Check that the PC is on and awake/)).toBeTruthy();

  agent.reach = 'ok';
  await user.press(screen.getByText('Try again'));
  expect(await screen.findByText('Fix the login bug')).toBeTruthy();
  await waitFor(() => expect(screen.queryByText("Can't reach the PC")).toBeNull());
});

test('the home card names the cause in one line', async () => {
  agent.reach = 'agent_down';
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText('The PC is on, but the agent is not running')).toBeTruthy();
});

test('what the PC reports about Claude shows above the list', async () => {
  agent.notices = ['claude_remote_control_disabled_env'];
  const c = await pairedPC();
  await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
  expect(await screen.findByText('An environment variable on the PC turns Remote Control off.')).toBeTruthy();
});

test('two PCs stay apart: each card opens its own PC with its own token (F7)', async () => {
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
  await pairedPC('LAPTOP', laptopHost, 'laptop-token');

  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText('DESKTOP-TEST')).toBeTruthy();
  expect(screen.getByText('LAPTOP')).toBeTruthy();
  await waitFor(() => expect(screen.getAllByText('Connected · agent 0.1.0')).toHaveLength(2));

  await user.press(screen.getByText('LAPTOP'));
  expect(await screen.findByText('Laptop work')).toBeTruthy();
  expect(screen.queryByText('Fix the login bug')).toBeNull();
});

test('the "ask all" entrance has its place but stays hidden', async () => {
  await renderRouter(appDir, { initialUrl: '/' });
  await screen.findByText('Settings');
  expect(screen.queryByText('Ask all')).toBeNull();
  // The screen itself exists for Phase 2.
  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByText(/comes in the next phase/)).toBeTruthy();
});
