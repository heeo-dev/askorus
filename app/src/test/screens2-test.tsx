// Screen tests for new session, folders and settings (B1-4).

import { userEvent } from '@testing-library/react-native';
import { fireEvent, renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Alert, Linking } from 'react-native';

import { savePrefs } from '@/connections/prefs';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';

beforeEach(() => {
  agent = new FakeAgent();
  agent.folders = [
    { id: 'shop', name: 'shop', path: 'D:\\work\\shop', isGit: true, server: { enabled: false, capacity: 4 } },
    { id: 'docs', name: 'docs', path: 'D:\\work\\docs', isGit: false, server: { enabled: false, capacity: 4 } },
  ];
  agent.sessions = [session({ id: 'claude:1', title: 'Existing work', state: 'stopped' })];
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

async function pairedPC() {
  return new ConnectionStore().add(
    {
      kind: 'pc',
      name: 'DESKTOP-TEST',
      host: HOST,
      port: PORT,
      deviceId: 'dev-1',
      deviceName: 'Test Phone',
      pairedAt: '2026-10-27T00:00:00Z',
    },
    TOKEN,
  );
}

const confirmDestructive = () =>
  jest.spyOn(Alert, 'alert').mockImplementation((_t, _m, buttons) => {
    buttons?.find((b) => b.style === 'destructive')?.onPress?.();
  });

describe('new session', () => {
  test('starts in the chosen folder and hands the link to the Claude app', async () => {
    const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
    await user.press(await screen.findByText('+ New session'));

    await user.press(await screen.findByRole('radio', { name: 'docs' }));
    await user.type(screen.getByPlaceholderText('Leave empty for the folder name and time'), 'release notes');
    await user.press(screen.getByRole('radio', { name: 'opus' }));
    await user.press(screen.getByText('Start and open in the Claude app'));

    await waitFor(() => expect(open).toHaveBeenCalledWith(agent.remoteUrl));
    expect(agent.callsTo('POST', '/v1/sessions')[0].body).toEqual({
      folder: 'docs',
      name: 'release notes',
      model: 'opus',
    });
    // Back on the list, which shows the new session.
    expect(await screen.findByText('release notes')).toBeTruthy();
  });

  test('uses the defaults from settings', async () => {
    jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    await savePrefs({ model: 'sonnet', effort: 'high' });
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}/new` });
    await user.press(await screen.findByRole('radio', { name: 'shop' }));
    await user.press(screen.getByText('Start and open in the Claude app'));
    await waitFor(() => expect(agent.callsTo('POST', '/v1/sessions')).toHaveLength(1));
    expect(agent.callsTo('POST', '/v1/sessions')[0].body).toMatchObject({ model: 'sonnet', effort: 'high' });
  });

  test('cannot start before a folder is picked', async () => {
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}/new` });
    await user.press(await screen.findByText('Start and open in the Claude app'));
    expect(agent.callsTo('POST', '/v1/sessions')).toHaveLength(0);
  });

  test('a PC without allowed folders says how to add one', async () => {
    agent.folders = [];
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}/new` });
    expect(await screen.findByText(/asksemble-agent folders add/)).toBeTruthy();
  });
});

describe('folders', () => {
  // The PUT answer still shows the old state; the screen must keep asking
  // until the server catches up (found by E2E-05, R-12).
  test('keeping a folder open follows the server until it is open, and back off', async () => {
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}/folders` });
    const toggle = await screen.findByLabelText('Keep shop open');
    await fireEvent(toggle, 'valueChange', true);
    expect(await screen.findByText('Open · 0/4 sessions', {}, { timeout: 5000 })).toBeTruthy();
    expect(agent.calls.find((x) => x.method === 'PUT')?.body).toEqual({ enabled: true });
    expect(agent.folders[0].server.enabled).toBe(true);

    await fireEvent(screen.getByLabelText('Keep shop open'), 'valueChange', false);
    await waitFor(() => expect(screen.queryByText('Open · 0/4 sessions')).toBeNull(), { timeout: 5000 });
    expect(screen.getAllByText('Off').length).toBeGreaterThan(0);
  });

  // R-15: turning a folder off leaves it in the Claude app, where a new
  // session waits until the folder is opened again (Claude Code keeps the
  // environment for about 4 hours). The screen says so.
  test('turning a folder off says what stays in the Claude app', async () => {
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}/folders` });
    expect(await screen.findByText(/drops out of the Claude app's list/)).toBeTruthy();
    expect(screen.queryByText(/still listed in the Claude app/)).toBeNull();

    await fireEvent(await screen.findByLabelText('Keep shop open'), 'valueChange', true);
    await screen.findByText('Open · 0/4 sessions', {}, { timeout: 5000 });
    expect(screen.queryByText(/still listed in the Claude app/)).toBeNull();
    await fireEvent(screen.getByLabelText('Keep shop open'), 'valueChange', false);
    expect(await screen.findByText(/still listed in the Claude app/)).toBeTruthy();
  });
});

describe('settings', () => {
  test('the connection check walks through what works', async () => {
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Check connection'));
    expect(await screen.findByText(/Reached the PC/)).toBeTruthy();
    expect(screen.getByText(/The PC knows this phone/)).toBeTruthy();
    expect(screen.getByText(/Agent 0\.1\.0/)).toBeTruthy();
    expect(screen.getByText(/Claude Code 2\.1\.310/)).toBeTruthy();
  });

  test('the check names each AI; one that only asks is optional (P2-1)', async () => {
    agent.askAgents = [
      { agent: 'codex', version: '0.164.0', available: true, capabilities: ['ask'] },
      { agent: 'opencode', available: false, capabilities: [], notices: ['opencode_not_found'] },
    ];
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Check connection'));
    expect(await screen.findByText(/Claude Code 2\.1\.310/)).toBeTruthy();
    expect(screen.getByText(/✓ Codex 0\.164\.0 \(asks\)/)).toBeTruthy();
    expect(screen.getByText(/· OpenCode: not on this PC \(left out of asks\)/)).toBeTruthy();
    expect(screen.queryByText(/✗/)).toBeNull();
    expect(screen.queryByText(/opencode_not_found/)).toBeNull();
  });

  test('an AI that is there but broken says why, still as a note', async () => {
    agent.askAgents = [{ agent: 'codex', available: false, capabilities: [], notices: ['codex_script_only'] }];
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Check connection'));
    expect(await screen.findByText(/· Codex: on the PC but not working/)).toBeTruthy();
    expect(screen.getByText(/Set codexPath in agent\.json/)).toBeTruthy();
    expect(screen.queryByText(/not on this PC/)).toBeNull();
    expect(screen.queryByText(/✗/)).toBeNull();
    expect(screen.queryByText(/codex_script_only/)).toBeNull();
  });

  test('the check shows what the PC reports wrong', async () => {
    agent.notices = ['claude_service_elevated'];
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Check connection'));
    expect(await screen.findByText(/claude daemon stop --any/)).toBeTruthy();
  });

  test('unpairing removes the phone on the PC and here', async () => {
    confirmDestructive();
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Unpair'));
    await waitFor(async () => expect(await new ConnectionStore().list()).toEqual([]));
    expect(agent.callsTo('DELETE', '/v1/devices/self')).toHaveLength(1);
    expect(screen.queryByText(/devices revoke/)).toBeNull();
  });

  test('unpairing while the PC is away says what is left on the PC', async () => {
    confirmDestructive();
    globalThis.fetch = (async () => {
      throw new TypeError('Network request failed');
    }) as typeof fetch;
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/settings' });
    await user.press(await screen.findByText('Unpair'));
    expect(await screen.findByText(/asksemble-agent devices revoke dev-1/)).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
  });

  test('home links to settings', async () => {
    const user = userEvent.setup();
    await renderRouter(appDir, { initialUrl: '/' });
    await user.press(await screen.findByText('Settings'));
    expect(screen).toHavePathname('/settings');
  });
});
