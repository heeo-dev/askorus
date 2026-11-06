// Screen tests (S5c) for a new session, folders and Settings, on the app's
// own layouts and screens (stand-ins for Ask and About, see screens-test).

import { userEvent } from '@testing-library/react-native';
import Constants from 'expo-constants';
import { router } from 'expo-router';
import { act, renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Alert, Linking, StyleSheet, Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import Pair from '@/app/pair';
import Folders from '@/app/pc/[id]/folders';
import NewSession from '@/app/pc/[id]/new';
import Settings from '@/app/settings';
import { conversations } from '@/ask/conversations';
import { features } from '@/ask/features';
import { loadAskPrefs } from '@/ask/prefs';
import { loadPrefs, savePrefs } from '@/connections/prefs';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';
import { loadAppearance } from '@/ui/appearance';
import { aiPalettes, palettes } from '@/ui/theme';

let agent: FakeAgent;

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
  'pc/[id]/new': NewSession,
  'pc/[id]/folders': Folders,
  settings: Settings,
  pair: Pair,
  about: () => <Text>about screen</Text>,
};

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

async function pairedPC(name = 'DESKTOP-TEST', host = HOST) {
  return new ConnectionStore().add(
    { kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: '2026-10-27T00:00:00Z' },
    TOKEN,
  );
}

const confirmDestructive = () =>
  jest.spyOn(Alert, 'alert').mockImplementation((_t, _m, buttons) => {
    buttons?.find((b) => b.style === 'destructive')?.onPress?.();
  });

describe('new session', () => {
  test('starts in the chosen folder with Advanced choices and hands the link to the Claude app', async () => {
    const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    const user = userEvent.setup();
    const c = await pairedPC();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(await screen.findByRole('button', { name: 'New session' }));

    // The top bar names the screen and the PC.
    expect(await screen.findByRole('header', { name: 'New session' })).toBeTruthy();
    expect(screen.getByText('DESKTOP-TEST')).toBeTruthy();

    await user.press(await screen.findByRole('radio', { name: 'docs' }));
    expect(screen.getByRole('radio', { name: 'docs' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'shop' })).not.toBeChecked();
    await user.type(screen.getByLabelText('Name'), 'release notes');

    // Model and effort wait under Advanced, which says what is picked.
    expect(screen.queryByRole('radio', { name: 'opus' })).toBeNull();
    const advanced = screen.getByRole('button', { name: 'Advanced, Model · effort' });
    expect(advanced).toHaveAccessibilityValue({ text: 'Default' });
    await user.press(advanced);
    expect(screen.getByRole('button', { name: 'Advanced, Model · effort' })).toBeExpanded();
    await user.press(screen.getByRole('radio', { name: 'opus' }));
    expect(screen.getByRole('button', { name: 'Advanced, Model · effort' })).toHaveAccessibilityValue({ text: 'opus' });
    expect(screen.getByText('It usually opens within 15 seconds.')).toBeTruthy();

    await user.press(screen.getByRole('button', { name: 'Start and open in the Claude app' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith(agent.remoteUrl));
    expect(agent.callsTo('POST', '/v1/sessions')[0].body).toEqual({ folder: 'docs', name: 'release notes', model: 'opus' });
    // Back on My PC, which shows the new session.
    await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
    expect(await screen.findByText('release notes')).toBeTruthy();
    expect(agent.callsTo('GET', '/v1/folders').length).toBeGreaterThanOrEqual(1);
    expect(c.id).toBeTruthy();
  });

  test('uses the defaults from Settings', async () => {
    jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    await savePrefs({ model: 'sonnet', effort: 'high' });
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    await user.press(await screen.findByRole('radio', { name: 'shop' }));
    expect(screen.getByRole('button', { name: 'Advanced, Model · effort' })).toHaveAccessibilityValue({ text: 'sonnet · high' });
    expect(screen.getByText('sonnet · high')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Start and open in the Claude app' }));
    await waitFor(() => expect(agent.callsTo('POST', '/v1/sessions')).toHaveLength(1));
    expect(agent.callsTo('POST', '/v1/sessions')[0].body).toMatchObject({ model: 'sonnet', effort: 'high' });
  });

  test('cannot start before a folder is picked', async () => {
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    await screen.findByRole('radio', { name: 'shop' });
    const start = screen.getByRole('button', { name: 'Start and open in the Claude app' });
    expect(start).toBeDisabled();
    await user.press(start);
    expect(agent.callsTo('POST', '/v1/sessions')).toHaveLength(0);
  });

  test('one folder is picked for you', async () => {
    agent.folders = agent.folders.slice(0, 1);
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    expect(await screen.findByRole('radio', { name: 'shop' })).toBeChecked();
    expect(screen.getByRole('button', { name: 'Start and open in the Claude app' })).toBeEnabled();
  });

  test('a PC without allowed folders says how to add one', async () => {
    agent.folders = [];
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    expect(await screen.findByText(/asksemble-agent folders add/)).toBeTruthy();
  });

  test('a start that takes long can be cancelled', async () => {
    const user = userEvent.setup();
    const fake = agent.fetch;
    // The PC keeps saying the job runs.
    globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(String(u)).pathname;
      if (path.startsWith('/v1/jobs/')) {
        return new Response(JSON.stringify({ jobId: path.split('/').pop(), kind: 'start', status: 'running', pollAfterMs: 1 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return fake(u, init);
    }) as typeof fetch;
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    await user.press(await screen.findByRole('radio', { name: 'shop' }));
    await user.press(screen.getByRole('button', { name: 'Start and open in the Claude app' }));
    expect(await screen.findByText('Starting a new session on the PC… usually under 15 seconds.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Start and open in the Claude app' })).toBeNull();
    await user.press(screen.getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByRole('button', { name: 'Start and open in the Claude app' })).toBeEnabled();
    expect(screen.queryByText('Starting a new session on the PC… usually under 15 seconds.')).toBeNull();
    expect(screen.getByText('It usually opens within 15 seconds.')).toBeTruthy();
  });

  test('a failed start says why and offers the button again', async () => {
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
    await user.press(await screen.findByRole('radio', { name: 'shop' }));
    agent.folders = agent.folders.filter((f) => f.id !== 'shop'); // removed on the PC meanwhile
    await user.press(screen.getByRole('button', { name: 'Start and open in the Claude app' }));
    expect(await screen.findByText("This folder isn't on the allowed list.")).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Start and open in the Claude app' })).toBeEnabled();
    expect(screen.queryByText('Starting a new session on the PC… usually under 15 seconds.')).toBeNull();
  });
});

describe('folders', () => {
  // The PUT answer still shows the old state; the screen must keep asking
  // until the server catches up (found by E2E-05, R-12).
  test('keeping a folder open follows the server until it is open, and back off', async () => {
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/folders` });
    expect(await screen.findByRole('header', { name: 'Folders' })).toBeTruthy();
    const toggle = await screen.findByRole('switch', { name: 'Keep shop open' });
    expect(toggle).not.toBeChecked();
    await user.press(toggle);
    expect(await screen.findByText('Open · 0/4 sessions', {}, { timeout: 5000 })).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Keep shop open' })).toBeChecked();
    expect(agent.calls.find((x) => x.method === 'PUT')?.body).toEqual({ enabled: true });
    expect(agent.folders[0].server.enabled).toBe(true);

    await user.press(screen.getByRole('switch', { name: 'Keep shop open' }));
    await waitFor(() => expect(screen.queryByText('Open · 0/4 sessions')).toBeNull(), { timeout: 5000 });
    expect(screen.getAllByText('Off').length).toBeGreaterThan(0);
  });

  // R-15: turning a folder off leaves it in the Claude app, where a new
  // session waits until the folder is opened again (Claude Code keeps the
  // environment for about 4 hours). The screen says so.
  test('turning a folder off says what stays in the Claude app', async () => {
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/folders` });
    expect(await screen.findByText(/drops out of the Claude app's list/)).toBeTruthy();
    expect(screen.queryByText(/still listed in the Claude app/)).toBeNull();

    await user.press(await screen.findByRole('switch', { name: 'Keep shop open' }));
    await screen.findByText('Open · 0/4 sessions', {}, { timeout: 5000 });
    expect(screen.queryByText(/still listed in the Claude app/)).toBeNull();
    await user.press(screen.getByRole('switch', { name: 'Keep shop open' }));
    expect(await screen.findByText(/still listed in the Claude app/)).toBeTruthy();
  });

  test('an agent without folder servers cannot switch them', async () => {
    agent.capabilities = agent.capabilities.filter((x) => x !== 'serve');
    const user = userEvent.setup();
    const c = await pairedPC();
    await renderRouter(routes, { initialUrl: `/pc/${c.id}/folders` });
    expect(await screen.findByText("This PC's agent doesn't support keeping folders open.")).toBeTruthy();
    const toggle = screen.getByRole('switch', { name: 'Keep shop open' });
    expect(toggle).toBeDisabled();
    await user.press(toggle);
    expect(agent.calls.filter((x) => x.method === 'PUT')).toHaveLength(0);
  });
});

describe('settings', () => {
  async function openPCSheet(user: ReturnType<typeof userEvent.setup>) {
    await renderRouter(routes, { initialUrl: '/settings' });
    await user.press(await screen.findByRole('button', { name: 'DESKTOP-TEST, Connected' }));
    return within(await screen.findByTestId('pc-sheet'));
  }

  test('the paired PCs show with their state; a PC opens its sheet with this phone’s name', async () => {
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    expect(pc.getByText(`${HOST}:${PORT}`)).toBeTruthy();
    expect(pc.getByText('This phone: Test Phone (id dev-1)')).toBeTruthy();
  });

  test('the connection check walks through what works', async () => {
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    await user.press(pc.getByRole('button', { name: 'Check connection' }));
    expect(await pc.findByText(/Reached the PC/)).toBeTruthy();
    expect(pc.getByText(/The PC knows this phone/)).toBeTruthy();
    expect(pc.getByText(/Agent 0\.1\.0/)).toBeTruthy();
    expect(pc.getByText(/Claude Code 2\.1\.310/)).toBeTruthy();
  });

  test('the check names each AI; one that only asks is optional (P2-1)', async () => {
    agent.askAgents = [
      { agent: 'codex', version: '0.164.0', available: true, capabilities: ['ask'] },
      { agent: 'opencode', available: false, capabilities: [], notices: ['opencode_not_found'] },
    ];
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    await user.press(pc.getByRole('button', { name: 'Check connection' }));
    expect(await pc.findByText(/Claude Code 2\.1\.310/)).toBeTruthy();
    expect(pc.getByText(/✓ Codex 0\.164\.0 \(asks\)/)).toBeTruthy();
    expect(pc.getByText(/· OpenCode: not on this PC \(left out of asks\)/)).toBeTruthy();
    expect(pc.queryByText(/✗/)).toBeNull();
    expect(pc.queryByText(/opencode_not_found/)).toBeNull();
  });

  test('an AI that is there but broken says why, still as a note', async () => {
    agent.askAgents = [{ agent: 'codex', available: false, capabilities: [], notices: ['codex_script_only'] }];
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    await user.press(pc.getByRole('button', { name: 'Check connection' }));
    expect(await pc.findByText(/· Codex: on the PC but not working/)).toBeTruthy();
    expect(pc.getByText(/Set codexPath in agent\.json/)).toBeTruthy();
    expect(pc.queryByText(/not on this PC/)).toBeNull();
    expect(pc.queryByText(/✗/)).toBeNull();
    expect(pc.queryByText(/codex_script_only/)).toBeNull();
  });

  test('the check shows what the PC reports wrong', async () => {
    agent.notices = ['claude_service_elevated'];
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    await user.press(pc.getByRole('button', { name: 'Check connection' }));
    expect(await pc.findByText(/claude daemon stop --any/)).toBeTruthy();
  });

  test('unpairing removes the phone on the PC and here', async () => {
    confirmDestructive();
    const user = userEvent.setup();
    await pairedPC();
    const pc = await openPCSheet(user);
    await user.press(pc.getByRole('button', { name: 'Unpair' }));
    await waitFor(async () => expect(await new ConnectionStore().list()).toEqual([]));
    expect(agent.callsTo('DELETE', '/v1/devices/self')).toHaveLength(1);
    await waitFor(() => expect(screen.queryByTestId('pc-sheet')).toBeNull());
    expect(screen.queryByText(/devices revoke/)).toBeNull();
    expect(await screen.findByText('No PC is paired yet.')).toBeTruthy();
  });

  test('unpairing while the PC is away says what is left on the PC', async () => {
    confirmDestructive();
    globalThis.fetch = (async () => {
      throw new TypeError('Network request failed');
    }) as typeof fetch;
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/settings' });
    await user.press(await screen.findByRole('button', { name: 'DESKTOP-TEST, Not connected' }));
    await user.press(within(await screen.findByTestId('pc-sheet')).getByRole('button', { name: 'Unpair' }));
    expect(await screen.findByText(/asksemble-agent devices revoke dev-1/)).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
  });

  test('new session defaults open in a sheet and are saved', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    const row = await screen.findByRole('button', { name: 'New session defaults' });
    expect(row).toHaveAccessibilityValue({ text: 'Default' });
    await user.press(row);
    const sheet = within(await screen.findByTestId('defaults-sheet'));
    await user.press(sheet.getByRole('radio', { name: 'haiku' }));
    await user.press(sheet.getByRole('radio', { name: 'max' }));
    await waitFor(async () => expect(await loadPrefs()).toEqual({ model: 'haiku', effort: 'max' }));
    await user.press(sheet.getByRole('button', { name: 'Close' }));
    expect(await screen.findByRole('button', { name: 'New session defaults' })).toHaveAccessibilityValue({ text: 'haiku · max' });
  });

  test('the default tier, the combined answer and its writer are saved in the ask prefs', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    await user.press(await screen.findByRole('radio', { name: 'Deep' }));
    await waitFor(async () => expect((await loadAskPrefs()).tier).toBe('deep'));
    expect(screen.getByRole('radio', { name: 'Deep' })).toBeChecked();

    // Since C6 the one switch: no comparing of its own to turn off.
    expect(screen.queryByRole('switch', { name: 'Compare answers automatically' })).toBeNull();
    const synth = screen.getByRole('switch', { name: 'Write a combined answer once the answers are in' });
    const synthWas = (await loadAskPrefs()).synth;
    expect(synth.props.accessibilityState.checked).toBe(synthWas);
    await user.press(synth);
    await waitFor(async () => expect((await loadAskPrefs()).synth).toBe(!synthWas));

    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked();
    await user.press(screen.getByRole('radio', { name: 'Codex' }));
    await waitFor(async () => expect((await loadAskPrefs()).synthAgent).toBe('codex'));
    expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked();
  });

  // 11-06: the moderator model, per moderator AI, from the current PC's
  // tier models; without that PC, it says so and does not open.
  test('without a PC, the moderator model follows the tier and says when it can be picked', async () => {
    await renderRouter(routes, { initialUrl: '/settings' });
    expect(await screen.findByText('You can pick it once the PC with this AI is connected.')).toBeTruthy();
    expect(screen.getByText('Moderator model')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Moderator model' })).toBeNull();
    expect(screen.getByText('By tier')).toBeTruthy();
  });

  test('with the PC connected, the moderator model opens its sheet and is saved for that AI', async () => {
    agent.enableAsk();
    agent.enableSynth();
    agent.askModels('codex', { quick: { model: 'gpt-6-luna', effort: 'medium' }, standard: { model: 'gpt-6.1-terra', effort: 'medium' } }, { 'gpt-6.1-terra': ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] });
    await pairedPC();
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    await user.press(await screen.findByRole('radio', { name: 'Codex' }));
    const row = await screen.findByRole('button', { name: 'Moderator model' });
    expect(row).toHaveAccessibilityValue({ text: 'By tier' });
    await user.press(row);
    expect(await screen.findByRole('header', { name: 'Moderator model: Codex' })).toBeTruthy();
    expect(screen.getByText('Writes the combined answer and leads debates with this model and effort at every tier.')).toBeTruthy();
    // Its own pick: terra at high; the sheet stays open.
    await user.press(within(screen.getByLabelText('Pick your own: Model')).getByRole('radio', { name: 'gpt-6.1-terra' }));
    await user.press(within(screen.getByLabelText('Pick your own: Effort')).getByRole('radio', { name: 'high' }));
    await waitFor(async () => expect((await loadAskPrefs()).synthModels).toEqual({ codex: { model: 'gpt-6.1-terra', effort: 'high' } }));
    await user.press(screen.getByRole('button', { name: 'Close' }));
    expect(await screen.findByRole('button', { name: 'Moderator model' })).toHaveAccessibilityValue({ text: 'gpt-6.1-terra · high' });
    // Kept per AI: Claude still follows the tier.
    await user.press(screen.getByRole('radio', { name: 'Claude' }));
    expect(await screen.findByRole('button', { name: 'Moderator model' })).toHaveAccessibilityValue({ text: 'By tier' });
  });

  // Milestone C: refining is off since 2026-11-04 (features.ts), and
  // Settings shows nothing of it: the moderator only writes the combined answer.
  test('milestone C: with the refining feature off, Settings has no refine switch or hint', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    expect(await screen.findByRole('header', { name: 'Moderator' })).toBeTruthy();
    expect(screen.queryByRole('switch', { name: 'Refine questions (add an answer format)' })).toBeNull();
    expect(screen.getByText('Moderator AI (writes the combined answer)')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'About the moderator' }));
    const info = within(await screen.findByTestId('compare-info'));
    expect(info.queryByText(/answers start about 5 seconds later/)).toBeNull();
    expect(info.queryByText(/judg/)).toBeNull();
    expect(info.getByText(/That AI's company receives the question and every answer/)).toBeTruthy();
  });

  describe('with the refining feature turned on', () => {
    beforeEach(() => {
      features.refine = true;
    });
    afterEach(() => {
      features.refine = false;
    });

    test('refining questions is off by default, under Moderator, and kept when turned on', async () => {
      const user = userEvent.setup();
      await renderRouter(routes, { initialUrl: '/settings' });
      expect(await screen.findByRole('header', { name: 'Moderator' })).toBeTruthy();
      const refine = screen.getByRole('switch', { name: 'Refine questions (add an answer format)' });
      expect(refine).not.toBeChecked();
      expect(screen.getByText('Moderator AI (refines questions, writes the combined answer)')).toBeTruthy();
      await user.press(refine);
      await waitFor(async () => expect((await loadAskPrefs()).refine).toBe(true));
      expect(screen.getByRole('switch', { name: 'Refine questions (add an answer format)' })).toBeChecked();
    });

    test('the info button explains what refining costs and sends', async () => {
      const user = userEvent.setup();
      await renderRouter(routes, { initialUrl: '/settings' });
      await user.press(await screen.findByRole('button', { name: 'About the moderator' }));
      const info = within(await screen.findByTestId('compare-info'));
      expect(info.getByText(/answers start about 5 seconds later\. The moderator AI's company receives the question once more\.$/)).toBeTruthy();
    });
  });

  test('the theme is system by default; dark and light are drawn at once and kept', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    expect(await screen.findByRole('radio', { name: 'System' })).toBeChecked();
    await user.press(screen.getByRole('radio', { name: 'Dark' }));
    expect(screen.getByRole('radio', { name: 'Dark' })).toBeChecked();
    await waitFor(async () => expect((await loadAppearance()).theme).toBe('dark'));
    expect(screen.getByRole('header', { name: 'Settings' })).toHaveStyle({ color: palettes.dark.text });

    await user.press(screen.getByRole('radio', { name: 'Light' }));
    await waitFor(async () => expect((await loadAppearance()).theme).toBe('light'));
    expect(screen.getByRole('header', { name: 'Settings' })).toHaveStyle({ color: palettes.light.text });
  });

  test('#6: AI colours are on by default; the switch turns them off at once and keeps it', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/settings' });
    const name = 'A color for each AI';
    expect(await screen.findByRole('switch', { name })).toBeChecked();
    // Codex's avatar on the writer pills (not the picked one).
    const fill = () => {
      let p = screen.getByText('Cx', { includeHiddenElements: true }).parent;
      for (; p; p = p.parent) if (StyleSheet.flatten(p.props.style)?.backgroundColor) return StyleSheet.flatten(p.props.style).backgroundColor;
    };
    expect(fill()).toBe(aiPalettes.light.lilac.bg);
    await user.press(screen.getByRole('switch', { name }));
    expect(screen.getByRole('switch', { name })).not.toBeChecked();
    expect(fill()).toBe(palettes.light.avatar);
    await waitFor(async () => expect(await loadAppearance()).toEqual({ theme: 'system', aiColours: false }));
  });

  test('milestone B: History counts the kept conversations and deletes them all after asking', async () => {
    const user = userEvent.setup();
    await conversations.save({ id: 'a', pcId: 'pc', startedAt: 1000, title: 'How tall is Everest?', turns: [] });
    await conversations.save({ id: 'b', pcId: 'pc', startedAt: 2000, title: 'Coffee per day?', turns: [] });
    await renderRouter(routes, { initialUrl: '/settings' });
    expect(await screen.findByText('Conversations kept: 2')).toBeTruthy();
    const alert = confirmDestructive();
    await user.press(screen.getByRole('button', { name: 'Delete all history' }));
    expect(alert.mock.calls[0].slice(0, 2)).toEqual([
      'Delete all history?',
      "Deletes every conversation kept on this phone (2). Each AI's own record on the PC stays.",
    ]);
    expect(await screen.findByText('No conversations kept.')).toBeTruthy();
    expect(conversations.list()).toEqual([]);
    expect(await conversations.load('a')).toBeUndefined();
  });

  test('About shows the version and the not-official line, and leads to the about screen', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/settings' });
    const version = Constants.expoConfig?.version ?? '';
    const about = await screen.findByRole('button', { name: `Asksemble ${version}`.trim() + ', Not an official product of any AI company.' });
    await user.press(about);
    await waitFor(() => expect(r.getPathname()).toBe('/about'));
  });

  test('Pair with a PC opens pairing', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/settings' });
    await user.press(await screen.findByRole('button', { name: 'Pair with a PC' }));
    await waitFor(() => expect(r.getPathname()).toBe('/pair'));
  });

  test('Back leaves Settings', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('No PC is paired yet.');
    // The drawer leads here; pushing it directly stands in for that.
    await act(async () => router.push('/settings'));
    await user.press(await screen.findByRole('button', { name: 'Back' }));
    await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
  });
});
