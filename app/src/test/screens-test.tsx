// Screen tests (S5c): 내 PC, the session sheet and pairing, driven like a
// person would against a pretend PC agent. The route tree is the app's own
// layouts and screens, with stand-ins for the Ask and About screens (their
// parts test those), so these tests do not depend on them. The shell also
// reads GET /v1/sessions for the waiting dot, so calls are counted by their
// query, never as exact totals.

import { userEvent } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { act, renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Alert, Linking, Text, View } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import Pair from '@/app/pair';
import Folders from '@/app/pc/[id]/folders';
import NewSession from '@/app/pc/[id]/new';
import Settings from '@/app/settings';
import { loadAskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { CODE, FakeAgent, HOST, PAIR_LINK, PORT, TOKEN, session } from '@/test/fakeAgent';
import { Header } from '@/ui/Header';

// The status bar shows its style as a test id.
jest.mock('expo-status-bar', () => ({
  ...jest.requireActual('expo-status-bar'),
  StatusBar: ({ style }: { style: string }) => require('react').createElement(require('react-native').View, { testID: `status-bar-${style}` }),
}));

let agent: FakeAgent;

beforeEach(() => {
  agent = new FakeAgent();
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  // The Ask stand-in has the real header: its 내 PC pill carries the waiting dot.
  '(tabs)/index': () => (
    <View>
      <Header entrance="ask" />
      <Text>ask screen</Text>
    </View>
  ),
  '(tabs)/pcs': MyPC,
  'pc/[id]/new': NewSession,
  'pc/[id]/folders': Folders,
  settings: Settings,
  pair: Pair,
  about: () => <Text>about screen</Text>,
};

async function pairedPC(name = 'DESKTOP-TEST', host = HOST) {
  return new ConnectionStore().add(
    { kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: '2026-10-27T00:00:00Z' },
    TOKEN,
  );
}

const sessionReads = (pred: (q: URLSearchParams) => boolean = () => true) => agent.callsTo('GET', '/v1/sessions').filter((c) => pred(c.query));
const sheet = () => within(screen.getByTestId('session-sheet'));

describe('My PC', () => {
  test('with no PC it explains how to pair, and the button opens pairing', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByText('No PC is paired yet.')).toBeTruthy();
    expect(screen.getByText(/asksemble-agent pair/)).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'My PCs' })).toBeSelected();
    await user.press(screen.getByRole('button', { name: 'Pair with a PC' }));
    await waitFor(() => expect(r.getPathname()).toBe('/pair'));
  });

  test('the current PC shows its name, state, agent version and sessions', async () => {
    agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' })];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByText('Fix the login bug')).toBeTruthy();
    expect(screen.getByText('DESKTOP-TEST')).toBeTruthy();
    expect(await screen.findByText('Connected · agent 0.1.0')).toBeTruthy();
    expect(screen.getByText('Sessions')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Pick another PC' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'New session' })).toBeTruthy();
  });

  test('a session waiting for input says so first, with its dot; running and stopped follow their state', async () => {
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', activity: 'blocked' }),
      session({ id: 'claude:2', title: 'Deploy the shop', state: 'running', activity: 'busy' }),
      session({ id: 'claude:3', title: 'Write release notes', state: 'stopped', lastActive: new Date(Date.now() - 2 * 3600_000).toISOString() }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByRole('button', { name: 'Fix the login bug, Waiting for input · just now' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Deploy the shop, Running · just now' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Write release notes, Stopped · 2 h ago' })).toBeTruthy();
    expect(screen.getByText('Waiting for input · just now')).toBeTruthy();
    expect(screen.getAllByTestId('dot-waiting', { includeHiddenElements: true })).toHaveLength(1);
    expect(screen.getAllByTestId('dot-running', { includeHiddenElements: true }).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByTestId('dot-stopped', { includeHiddenElements: true }).length).toBeGreaterThanOrEqual(1);
    // The header's 내 PC carries the waiting dot from this list (no filter, no search),
    // seen from 한 번에: on 내 PC itself its own pill has no dot (UX-5).
    expect(screen.getByRole('tab', { name: 'My PCs' })).toBeSelected();
    expect(screen.queryByTestId('waiting-dot')).toBeNull();
    await userEvent.setup().press(screen.getByRole('tab', { name: 'All at once' }));
    expect(await screen.findByRole('tab', { name: 'My PCs, Sessions waiting for input: 1' })).toBeTruthy();
  });

  test('a filtered list does not change the waiting dot; only a full list is handed to the shell', async () => {
    const user = userEvent.setup();
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', activity: 'blocked' }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    await user.press(screen.getByRole('button', { name: 'Stopped' }));
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());
    expect(sessionReads((q) => q.get('state') === 'stopped').length).toBeGreaterThanOrEqual(1);
    await user.press(screen.getByRole('tab', { name: 'All at once' }));
    expect(await screen.findByRole('tab', { name: 'My PCs, Sessions waiting for input: 1' })).toBeTruthy();
  });

  test('the state filter asks the PC for that state only', async () => {
    const user = userEvent.setup();
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    expect(screen.getByRole('button', { name: 'All' })).toBeSelected();
    await user.press(screen.getByRole('button', { name: 'Stopped' }));
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());
    expect(screen.getByText('Write release notes')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Stopped' })).toBeSelected();
    expect(sessionReads((q) => q.get('state') === 'stopped').length).toBeGreaterThanOrEqual(1);
  });

  test('search opens from the header, waits for typing to settle, then asks the PC', async () => {
    const user = userEvent.setup();
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    expect(screen.queryByPlaceholderText('Search titles, folders, last prompts')).toBeNull();
    await user.press(screen.getByRole('button', { name: 'Search sessions' }));
    await user.type(screen.getByPlaceholderText('Search titles, folders, last prompts'), 'release');
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());
    const searches = sessionReads((q) => q.has('q'));
    expect(searches.at(-1)?.query.get('q')).toBe('release');
    expect(searches.length).toBeLessThan('release'.length); // not one request per letter

    // Closing the search shows everything again.
    await user.press(screen.getByRole('button', { name: 'Search sessions' }));
    expect(await screen.findByText('Fix the login bug')).toBeTruthy();
    expect(screen.queryByPlaceholderText('Search titles, folders, last prompts')).toBeNull();
  });

  test('Ask chats shows only the sessions of asks; the other chips leave them out', async () => {
    const user = userEvent.setup();
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' }),
      session({ id: 'claude:5', title: 'Asked about HTTPS', state: 'history', badges: ['ask'] }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    expect(screen.queryByText('Asked about HTTPS')).toBeNull();
    await user.press(screen.getByRole('button', { name: 'Ask chats' }));
    expect(await screen.findByText('Asked about HTTPS')).toBeTruthy();
    expect(screen.queryByText('Fix the login bug')).toBeNull();
  });

  test('an empty list says so, and a filtered one says nothing matches', async () => {
    const user = userEvent.setup();
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByText('No sessions.')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Running' }));
    expect(await screen.findByText('No sessions match.')).toBeTruthy();
  });

  test('folders kept open show under Folders, and Keep folders open leads to the folders screen', async () => {
    const user = userEvent.setup();
    agent.folders = [
      { id: 'shop', name: 'shop', path: 'D:\\work\\shop', isGit: true, server: { enabled: true, capacity: 4 }, status: { state: 'running', sessions: 1 } },
      { id: 'docs', name: 'docs', path: 'D:\\work\\docs', isGit: false, server: { enabled: false, capacity: 4 } },
    ];
    const c = await pairedPC();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    expect(await screen.findByRole('button', { name: 'shop, Open' })).toBeTruthy();
    expect(screen.queryByText('docs')).toBeNull();
    expect(screen.getByText('Folders')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Keep folders open' }));
    await waitFor(() => expect(r.getPathname()).toBe(`/pc/${c.id}/folders`));
    expect(await screen.findByRole('switch', { name: 'Keep docs open' })).toBeTruthy();
  });

  test('New session opens the new session screen for this PC', async () => {
    const user = userEvent.setup();
    agent.folders = [{ id: 'shop', name: 'shop', path: 'D:\\work\\shop', isGit: true, server: { enabled: false } }];
    const c = await pairedPC();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(await screen.findByRole('button', { name: 'New session' }));
    await waitFor(() => expect(r.getPathname()).toBe(`/pc/${c.id}/new`));
    expect(await screen.findByText('Start and open in the Claude app')).toBeTruthy();
  });

  test('the drawer’s waiting session opens its sheet, even under a filter, then the param is cleared', async () => {
    const user = userEvent.setup();
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', activity: 'blocked' }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' }),
    ];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
    await user.press(screen.getByRole('button', { name: 'Stopped' }));
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());

    await act(async () => router.navigate({ pathname: '/pcs', params: { session: 'claude:1' } }));
    expect(await screen.findByTestId('session-sheet')).toBeTruthy();
    expect(sheet().getByText('Fix the login bug')).toBeTruthy();
    expect(sheet().getByText('Waiting for input · just now')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'All' })).toBeSelected();
    await waitFor(() => expect(screen).toHaveSearchParams({}));
  });

  test('a session param on opening the app waits for the PC, then opens that sheet', async () => {
    agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', activity: 'blocked' })];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs?session=claude%3A1' });
    expect(await screen.findByTestId('session-sheet')).toBeTruthy();
    expect(sheet().getByText('Fix the login bug')).toBeTruthy();
  });

  test('a session param for a session that is gone opens nothing', async () => {
    agent.sessions = [session({ id: 'claude:1', title: 'Fix the login bug', state: 'running' })];
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs?session=claude%3Agone' });
    await screen.findByText('Fix the login bug');
    await waitFor(() => expect(screen).toHaveSearchParams({}));
    expect(screen.queryByTestId('session-sheet')).toBeNull();
  });
});

describe('session sheet', () => {
  beforeEach(() => {
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', folder: 'D:\\work\\shop', model: 'sonnet', badges: ['rc'], attachCommand: 'claude --resume abc' }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped', folder: 'D:\\work\\docs' }),
      session({ id: 'claude:3', title: 'Old experiment', state: 'history', folder: 'D:\\gone', badges: ['folder_missing'], openModes: [] }),
    ];
  });

  async function openPC() {
    await pairedPC();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await screen.findByText('Fix the login bug');
  }

  test('shows the state, folder, model and badges as tags', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Fix the login bug'));
    expect(sheet().getByText('Running · just now')).toBeTruthy();
    expect(sheet().getByLabelText('Folder shop')).toBeTruthy();
    expect(sheet().getByLabelText('Model sonnet')).toBeTruthy();
    expect(sheet().getByText('Remote on')).toBeTruthy();
    expect(sheet().getByRole('header', { name: 'Fix the login bug' })).toBeTruthy();
  });

  test('opening a stopped session hands the link to the Claude app', async () => {
    const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    await user.press(sheet().getByRole('button', { name: 'Open in the Claude app' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith(agent.remoteUrl));
    expect(await sheet().findByText('Woke the stopped session.')).toBeTruthy();
    expect(sheet().getByRole('button', { name: 'Open again' })).toBeTruthy();
    expect(agent.callsTo('POST', '/v1/sessions/claude%3A2/open')).toHaveLength(1);
  });

  test('stop is offered only for running sessions', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    expect(sheet().queryByRole('button', { name: 'Stop' })).toBeNull();
    expect(sheet().getByRole('button', { name: 'Remove' })).toBeTruthy();
    await user.press(sheet().getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByTestId('session-sheet')).toBeNull());
    await user.press(screen.getByText('Fix the login bug'));
    await user.press(sheet().getByRole('button', { name: 'Stop' }));
    expect(await sheet().findByText('Stopped. Opening it again resumes it.')).toBeTruthy();
    expect(agent.sessions.find((s) => s.id === 'claude:1')?.state).toBe('stopped');
    expect(sheet().getByText('Stopped · just now')).toBeTruthy();
  });

  test('tapping the dimmed screen closes the sheet', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    await user.press(screen.getByTestId('session-sheet-scrim', { includeHiddenElements: true }));
    await waitFor(() => expect(screen.queryByTestId('session-sheet')).toBeNull());
  });

  test('remove asks first, then drops the session from the list', async () => {
    const alert = jest.spyOn(Alert, 'alert').mockImplementation((_t, _m, buttons) => {
      buttons?.find((b) => b.style === 'destructive')?.onPress?.();
    });
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    await user.press(sheet().getByRole('button', { name: 'Remove' }));
    expect(alert).toHaveBeenCalledWith('Remove this session?', expect.any(String), expect.any(Array));
    await waitFor(() => expect(screen.queryByText('Write release notes')).toBeNull());
    expect(agent.sessions.map((s) => s.id)).toEqual(['claude:1', 'claude:3']);
  });

  test('a session whose folder is gone explains why it cannot open', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Old experiment'));
    expect(await sheet().findByText(/folder was deleted from the PC/)).toBeTruthy();
    expect(sheet().queryByRole('button', { name: 'Open in the Claude app' })).toBeNull();
    expect(sheet().getByRole('button', { name: 'Remove' })).toBeTruthy();
    expect(sheet().getByText('Folder gone')).toBeTruthy();
  });

  test('the command that continues the session on the PC is copied', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Fix the login bug'));
    await user.press(sheet().getByRole('button', { name: 'Copy the command to continue on the PC' }));
    expect(Clipboard.setStringAsync).toHaveBeenCalledWith('claude --resume abc');
    expect(await sheet().findByText('Copied.')).toBeTruthy();
    // A session without one has no such row.
    await user.press(sheet().getByRole('button', { name: 'Close' }));
    await user.press(await screen.findByText('Write release notes'));
    expect(sheet().queryByRole('button', { name: 'Copy the command to continue on the PC' })).toBeNull();
  });
});

describe('pairing', () => {
  test('the QR is the main way: run the command, then scan; the camera is asked for first', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(await screen.findByRole('button', { name: 'Pair with a PC' }));
    expect(await screen.findByRole('header', { name: 'Pair with a PC' })).toBeTruthy();
    expect(screen.getByText('asksemble-agent pair')).toBeTruthy();
    expect(screen.getByText('Run this in a terminal on the PC')).toBeTruthy();
    expect(screen.getByText('Scan the QR on the PC')).toBeTruthy();
    // The link is the second way, behind a quiet button; there is no code entry.
    expect(screen.queryByPlaceholderText('asksemble://pair?…')).toBeNull();
    expect(screen.queryByText(/enter.*code/i)).toBeNull();
    expect(screen.getByText('Not an official product of any AI company.')).toBeTruthy();

    await user.press(screen.getByRole('button', { name: 'Scan QR code' }));
    expect(await screen.findByText('The camera is needed to scan the QR code on the PC screen.')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText('asksemble-agent pair')).toBeTruthy();
  });

  // The camera is dark in either theme; in the light one the status icons
  // were dark on it and could not be seen (S10, 11-03).
  test('the scanner puts light status icons over the camera', async () => {
    jest.spyOn(require('expo-camera'), 'useCameraPermissions').mockReturnValue([{ granted: true, canAskAgain: true }, jest.fn()]);
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pair' });
    expect(screen.queryByTestId('status-bar-light')).toBeNull();
    await user.press(await screen.findByRole('button', { name: 'Scan QR code' }));
    expect(await screen.findByText('Fit the QR code on the PC screen inside the frame.')).toBeTruthy();
    expect(screen.getByTestId('status-bar-light')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByTestId('status-bar-light')).toBeNull());
  });

  test('copy puts the command on the clipboard', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pair' });
    await user.press(await screen.findByRole('button', { name: 'Copy command' }));
    expect(Clipboard.setStringAsync).toHaveBeenCalledWith('asksemble-agent pair');
    expect(await screen.findByText('Copied.')).toBeTruthy();
  });

  test('pasting the link pairs, saves the PC and shows it in My PC', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(await screen.findByRole('button', { name: 'Pair with a PC' }));
    await user.press(await screen.findByRole('button', { name: 'Paste the link instead' }));
    await user.type(screen.getByLabelText('The link on the PC'), PAIR_LINK);
    await user.press(screen.getByRole('button', { name: 'Pair with this link' }));

    // The phone shows which PC before sending anything.
    expect(await screen.findByText(HOST)).toBeTruthy();
    expect(agent.calls).toHaveLength(0);

    await user.press(screen.getByRole('button', { name: 'Pair' }));
    expect(await screen.findByText('Connected · agent 0.1.0')).toBeTruthy();
    await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
    expect(screen.getByText('DESKTOP-TEST')).toBeTruthy();
    expect(agent.callsTo('POST', '/v1/pair')[0].body).toEqual({ code: CODE, deviceName: 'Test Phone' });

    const [saved] = await new ConnectionStore().list();
    expect(saved).toMatchObject({ host: HOST, port: PORT, name: 'DESKTOP-TEST' });
    expect(await new ConnectionStore().token(saved.id)).toBe(TOKEN);
  });

  test('the paste button fills the field from the clipboard', async () => {
    jest.mocked(Clipboard.getStringAsync).mockResolvedValueOnce(`  ${PAIR_LINK}\n`);
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pair' });
    await user.press(await screen.findByRole('button', { name: 'Paste the link instead' }));
    await user.press(screen.getByRole('button', { name: 'Paste' }));
    await waitFor(() => expect(screen.getByLabelText('The link on the PC').props.value).toBe(PAIR_LINK));
    await user.press(screen.getByRole('button', { name: 'Pair with this link' }));
    expect(await screen.findByText('Is this your PC?')).toBeTruthy();
  });

  test('a newly paired PC becomes the current one', async () => {
    const user = userEvent.setup();
    await pairedPC('OLD-LAPTOP', 'old-laptop.tail0.ts.net');
    await renderRouter(routes, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    await user.press(await screen.findByRole('button', { name: 'Pair' }));
    expect(await screen.findByText('DESKTOP-TEST')).toBeTruthy();
    expect(screen.queryByText('OLD-LAPTOP')).toBeNull();
    const [, added] = await new ConnectionStore().list();
    expect((await loadAskPrefs()).pc).toBe(added.id);
  });

  test('the pairing link itself opens the confirm step, with no back button', async () => {
    await renderRouter(routes, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    expect(await screen.findByText('Is this your PC?')).toBeTruthy();
    expect(screen.getByText(HOST)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Back' })).toBeNull();
    expect(screen.getByRole('header', { name: 'Asksemble' })).toBeTruthy();
  });

  test('opened from My PC it can go back', async () => {
    const user = userEvent.setup();
    const r = await renderRouter(routes, { initialUrl: '/pcs' });
    await user.press(await screen.findByRole('button', { name: 'Pair with a PC' }));
    await user.press(await screen.findByRole('button', { name: 'Back' }));
    await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
  });

  test('a refusal on the PC is explained and nothing is saved', async () => {
    const user = userEvent.setup();
    agent.pairAnswer = 'deny';
    await renderRouter(routes, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    await user.press(await screen.findByRole('button', { name: 'Pair' }));
    expect(await screen.findByText('Declined on the PC.')).toBeTruthy();
    expect(screen.getByText("Couldn't pair")).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
    await user.press(screen.getByRole('button', { name: 'Start over' }));
    expect(await screen.findByText('asksemble-agent pair')).toBeTruthy();
  });

  test('no answer on the PC in time is explained', async () => {
    const user = userEvent.setup();
    const fake = agent.fetch;
    globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
      if (new URL(String(u)).pathname === '/v1/pair/status') {
        return new Response(JSON.stringify({ error: { code: 'pair_timeout', message: 'timeout' } }), { status: 410, headers: { 'Content-Type': 'application/json' } });
      }
      return fake(u, init);
    }) as typeof fetch;
    await renderRouter(routes, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    await user.press(await screen.findByRole('button', { name: 'Pair' }));
    expect(await screen.findByText('No answer on the PC within 2 minutes.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Start over' })).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
  });

  test('waiting for the PC can be cancelled', async () => {
    const user = userEvent.setup();
    agent.pairAnswer = 'wait';
    await renderRouter(routes, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    await user.press(await screen.findByRole('button', { name: 'Pair' }));
    expect(await screen.findByText('Waiting for the PC to allow it')).toBeTruthy();
    await user.press(screen.getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText('Is this your PC?')).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
  });

  test('a link that is not a pairing link is refused', async () => {
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pair' });
    await user.press(await screen.findByRole('button', { name: 'Paste the link instead' }));
    await user.type(screen.getByLabelText('The link on the PC'), 'https://example.com/');
    await user.press(screen.getByRole('button', { name: 'Pair with this link' }));
    expect(await screen.findByText('Not a pairing link. Paste the whole link shown on the PC.')).toBeTruthy();
  });

  test('the Tailscale line opens a sheet with what to do and where to get it', async () => {
    const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    const user = userEvent.setup();
    await renderRouter(routes, { initialUrl: '/pair' });
    await user.press(await screen.findByRole('button', { name: 'The phone and the PC need the same Tailscale account turned on' }));
    const ts = within(await screen.findByTestId('tailscale-sheet'));
    expect(ts.getByText(/same Tailscale account as the PC/)).toBeTruthy();
    await user.press(ts.getByRole('button', { name: 'Get Tailscale' }));
    expect(open).toHaveBeenCalledWith(expect.stringContaining('tailscale'));
  });
});
