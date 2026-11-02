// S5c: the frame around the screens, with the real root and (tabs) layouts
// and stand-in screens: the app opens on 한 번에, the header switches between
// the two entrances without losing either, the drawer shows this run's
// questions, the current PC and its sessions waiting for input, and leads
// to them and to Settings; the theme choice is drawn and kept.

import { userEvent } from '@testing-library/react-native';
import { router, useLocalSearchParams } from 'expo-router';
import { act, renderHook, renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { useState } from 'react';
import { StatusBar, Text, TextInput, View } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import RootLayout from '@/app/_layout';
import { conversations, useConversations } from '@/ask/conversations';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';
import { Segment } from '@/ui/controls';
import { Header, TopBar } from '@/ui/Header';
import { themeChoices, useTheme, useThemeChoice } from '@/ui/theme';

const mockScheme = { value: 'light' as 'light' | 'dark' | null };
jest.mock('react-native/Libraries/Utilities/useColorScheme', () => ({ __esModule: true, default: () => mockScheme.value }));

let agent: FakeAgent;

beforeEach(() => {
  mockScheme.value = 'light';
  agent = new FakeAgent();
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

function Ask() {
  const th = useTheme();
  const [draft, setDraft] = useState('');
  const { openRequest } = useConversations();
  return (
    <View style={{ flex: 1, backgroundColor: th.bg }} testID="ask-bg">
      <Header entrance="ask" />
      <Text>ask screen</Text>
      <TextInput accessibilityLabel="draft" value={draft} onChangeText={setDraft} />
      <Text>{`open request: ${openRequest ?? 'none'}`}</Text>
    </View>
  );
}

function PCs() {
  const { session: id } = useLocalSearchParams<{ session?: string }>();
  return (
    <View style={{ flex: 1 }}>
      <Header entrance="pcs" />
      <Text>{`pcs screen ${id ?? ''}`}</Text>
    </View>
  );
}

function Settings() {
  const th = useTheme();
  const { choice, setChoice } = useThemeChoice();
  return (
    <View style={{ flex: 1, backgroundColor: th.bg }} testID="settings-bg">
      <TopBar title="Settings" onBack={() => router.back()} />
      <Segment options={themeChoices} value={choice} onChange={setChoice} labelOf={(c) => c} look="setting" label="Theme" />
    </View>
  );
}

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': Ask,
  '(tabs)/pcs': PCs,
  settings: Settings,
  pair: () => <Text>pair screen</Text>,
};

async function pairedPC(name = 'DESKTOP-TEST') {
  return new ConnectionStore().add({ kind: 'pc', name, host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
}

const sessionsCalls = () => agent.callsTo('GET', '/v1/sessions');
const day = (daysAgo: number, hour = 10) => {
  const n = new Date();
  return new Date(n.getFullYear(), n.getMonth(), n.getDate() - daysAgo, hour).getTime();
};

test('the app opens on 한 번에, and the header switches without losing either entrance', async () => {
  const user = userEvent.setup();
  const r = await renderRouter(routes, { initialUrl: '/' });
  expect(await screen.findByText('ask screen')).toBeTruthy();
  expect(r.getPathname()).toBe('/');
  expect(screen.getByRole('tab', { name: 'All at once' })).toBeSelected();
  expect(screen.getByRole('tab', { name: 'My PCs' })).not.toBeSelected();

  await user.type(screen.getByLabelText('draft'), 'half a question');
  await user.press(screen.getByRole('tab', { name: 'My PCs' }));
  expect(await screen.findByText('pcs screen')).toBeTruthy();
  expect(r.getPathname()).toBe('/pcs');
  expect(screen.getByRole('tab', { name: 'My PCs' })).toBeSelected();

  await user.press(screen.getByRole('tab', { name: 'All at once' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
  expect(screen.getByLabelText('draft').props.value).toBe('half a question'); // still mounted

  // Back from 내 PC goes to 한 번에 (the tabs keep their history).
  await user.press(screen.getByRole('tab', { name: 'My PCs' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
  await waitFor(() => expect(router.canGoBack()).toBe(true));
  router.back();
  await waitFor(() => expect(r.getPathname()).toBe('/'));
});

test('the drawer shows this run’s questions by day and reopens one', async () => {
  const user = userEvent.setup();
  conversations.save({ id: 'old', pcId: 'pc', startedAt: day(1), title: 'How tall is Everest?', turns: [] });
  conversations.save({ id: 'older', pcId: 'pc', startedAt: day(5), title: 'Coffee per day?', turns: [] });
  conversations.setCurrent({ id: 'now', startedAt: day(0, 0) + 60000, title: 'How long do EV batteries last?' });
  const r = await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));

  expect(await screen.findByText('Today')).toBeTruthy();
  expect(screen.getByText('Yesterday')).toBeTruthy();
  const d = new Date(day(5));
  expect(screen.getByText(`${d.getMonth() + 1}/${d.getDate()}`)).toBeTruthy();
  expect(screen.getByRole('button', { name: 'How long do EV batteries last?' })).toBeSelected();
  expect(screen.getByText('Not an official product of any AI company.')).toBeTruthy();
  expect(screen.getByText('Asksemble')).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'How tall is Everest?' }));
  expect(await screen.findByText('open request: old')).toBeTruthy();
  await waitFor(() => expect(screen.queryByTestId('drawer')).toBeNull());
  expect(r.getPathname()).toBe('/');

  // The current one only closes the drawer.
  conversations.takeOpenRequest();
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(await screen.findByRole('button', { name: 'How long do EV batteries last?' }));
  await waitFor(() => expect(screen.queryByTestId('drawer')).toBeNull());
  expect(screen.getByText('open request: none')).toBeTruthy();
});

test('an empty drawer says the questions go when the app closes, and it closes', async () => {
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  expect(await screen.findByText('Questions you ask show here until the app closes.')).toBeTruthy();
  // No PC paired: the PC block offers pairing.
  expect(await screen.findByRole('button', { name: 'Pair with a PC' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Close menu' }));
  await waitFor(() => expect(screen.queryByTestId('drawer')).toBeNull());
});

// R-64: on the S10 (Android 12) the drawer's window got white status bar
// icons over its light panel; it sets the theme's look once it shows.
test.each([
  ['light', 'dark-content'],
  ['dark', 'light-content'],
] as const)('the drawer sets the %s theme’s status bar look', async (scheme, look) => {
  mockScheme.value = scheme;
  const set = jest.spyOn(StatusBar, 'setBarStyle').mockImplementation(() => undefined);
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  let modal = (await screen.findByTestId('drawer')).parent;
  while (modal && modal.props.onShow === undefined) modal = modal.parent;
  set.mockClear();
  await act(async () => modal?.props.onShow());
  expect(set).toHaveBeenCalledWith(look);
});

test('the current PC and its sessions waiting for input: header dot, drawer rows, and where they lead', async () => {
  const user = userEvent.setup();
  await pairedPC();
  agent.sessions = [
    session({ id: 'claude:w1', title: 'Polish the screens', state: 'running', activity: 'blocked' }),
    session({ id: 'claude:ask', title: 'an ask', state: 'running', activity: 'blocked', badges: ['ask'] }), // asks never count
    session({ id: 'claude:busy', title: 'Working', state: 'running', activity: 'busy' }),
  ];
  const r = await renderRouter(routes, { initialUrl: '/' });

  // The shell reads the PC's sessions (no filter) and the header shows one waiting.
  expect(await screen.findByRole('tab', { name: 'My PCs, Sessions waiting for input: 1' })).toBeTruthy();
  expect(screen.getByTestId('waiting-dot')).toBeTruthy();
  expect(sessionsCalls().length).toBeGreaterThanOrEqual(1);
  expect(sessionsCalls().every((c) => !c.query.get('state') && !c.query.get('q'))).toBe(true);

  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  expect(await screen.findByRole('button', { name: 'DESKTOP-TEST, Connected' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Polish the screens, Waiting for input' })).toBeTruthy();
  expect(screen.queryByText('an ask')).toBeNull();
  expect(screen.queryByText('Working')).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Polish the screens, Waiting for input' }));
  expect(await screen.findByText('pcs screen claude:w1')).toBeTruthy();
  expect(r.getPathname()).toBe('/pcs');
  expect(r.getSearchParams()).toMatchObject({ session: 'claude:w1' });
  await waitFor(() => expect(screen.queryByTestId('drawer')).toBeNull());
  // On 내 PC itself no dot on its own (selected) pill: its list shows who waits (UX-5).
  expect(screen.getByRole('tab', { name: 'My PCs' })).toBeSelected();
  expect(screen.queryByTestId('waiting-dot')).toBeNull();

  await user.press(screen.getByRole('tab', { name: 'All at once' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
  expect(screen.getByRole('tab', { name: 'My PCs, Sessions waiting for input: 1' })).not.toBeSelected();
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(await screen.findByRole('button', { name: 'DESKTOP-TEST, Connected' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pcs'));
  await user.press(screen.getByRole('tab', { name: 'All at once' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
  expect(screen.getByTestId('waiting-dot')).toBeTruthy();

  // Once nothing waits, the dot goes: opening the drawer reads the PC again
  // (TEST-3), once the last read is older than 5 s (no screen handed a list over).
  agent.sessions = agent.sessions.map((x) => ({ ...x, activity: 'busy' }));
  const reads = sessionsCalls().length;
  const realNow = Date.now.bind(Date);
  jest.spyOn(Date, 'now').mockImplementation(() => realNow() + 6000);
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await waitFor(() => expect(sessionsCalls().length).toBe(reads + 1));
  await waitFor(() => expect(screen.queryByTestId('waiting-dot')).toBeNull());
  expect(screen.getByRole('tab', { name: 'My PCs' })).toBeTruthy();
  expect(await screen.findByRole('button', { name: 'DESKTOP-TEST, Connected' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Polish the screens, Waiting for input' })).toBeNull();
});

test('a PC that does not answer shows as not connected, with no waiting rows', async () => {
  const user = userEvent.setup();
  await pairedPC();
  agent.reach = 'offline';
  await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  expect(await screen.findByRole('button', { name: 'DESKTOP-TEST, Not connected' })).toBeTruthy();
});

test('the drawer leads to Settings, whose back returns', async () => {
  const user = userEvent.setup();
  const r = await renderRouter(routes, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  await user.press(await screen.findByRole('button', { name: 'Settings' }));
  await waitFor(() => expect(r.getPathname()).toBe('/settings'));
  expect(await screen.findByRole('header', { name: 'Settings' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Back' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
});

test('the theme choice is drawn at once and kept: dark, light, system', async () => {
  const user = userEvent.setup();
  const bg = (id: string) => [screen.getByTestId(id).props.style].flat(3).reduce((c, x) => (x && x.backgroundColor ? x.backgroundColor : c), '');
  const r = await renderRouter(routes, { initialUrl: '/settings' });
  expect(await screen.findByRole('radio', { name: 'system' })).toBeChecked();
  expect(bg('settings-bg')).toBe('#FCFAF8'); // the phone is light

  await user.press(screen.getByRole('radio', { name: 'dark' }));
  expect(bg('settings-bg')).toBe('#151618');
  expect(JSON.parse((await require('expo-secure-store').getItemAsync('ui.v1')) ?? '')).toEqual({ theme: 'dark' });

  await user.press(screen.getByRole('radio', { name: 'light' }));
  expect(bg('settings-bg')).toBe('#FCFAF8');

  mockScheme.value = 'dark';
  await user.press(screen.getByRole('radio', { name: 'system' }));
  expect(bg('settings-bg')).toBe('#151618'); // follows the (now dark) phone

  // Kept for the next start.
  await user.press(screen.getByRole('radio', { name: 'light' }));
  await r.unmount();
  mockScheme.value = 'dark';
  await renderRouter(routes, { initialUrl: '/' });
  await waitFor(() => expect(bg('ask-bg')).toBe('#FCFAF8'));
});

describe('the current PC and its status, without screens', () => {
  const { currentPC, pickPC, reloadPCs, currentPCId } = require('@/ui/shell/pcs') as typeof import('@/ui/shell/pcs');
  const status = require('@/ui/shell/pcStatus') as typeof import('@/ui/shell/pcStatus');
  const { loadAskPrefs, saveAskPrefs, defaultAskPrefs } = require('@/ask/prefs') as typeof import('@/ask/prefs');
  const { CancelledError, NetworkError } = require('@/api/client') as typeof import('@/api/client');

  test('the current PC is the one the ask prefs name, else the first; picking saves it', async () => {
    expect(await currentPC()).toBeNull();
    const a = await pairedPC('PC-A');
    const b = await new ConnectionStore().add({ kind: 'pc', name: 'PC-B', host: 'b.tail0.ts.net', port: PORT, deviceId: 'd', deviceName: 'p', pairedAt: 'x' }, 't');
    expect((await currentPC())?.conn.id).toBe(a.id);
    await saveAskPrefs({ ...defaultAskPrefs, pc: 'gone' });
    expect((await currentPC())?.conn.id).toBe(a.id); // a PC no longer paired falls back to the first
    await pickPC(b.id);
    expect(currentPCId()).toBe(b.id);
    expect((await loadAskPrefs()).pc).toBe(b.id);
    expect((await currentPC())?.conn.name).toBe('PC-B');
    await reloadPCs();
    expect(currentPCId()).toBe(b.id);
  });

  test('a list a screen just handed over is not read again for 30 s; force reads anyway', async () => {
    const pc = await pairedPC();
    await reloadPCs();
    agent.sessions = [session({ id: 'claude:w', activity: 'blocked' })];
    status.publishSessions(pc.id, agent.sessions);
    await status.refreshPCStatus();
    expect(sessionsCalls()).toHaveLength(0);
    await status.refreshPCStatus({ force: true });
    expect(sessionsCalls()).toHaveLength(1);
    // A read of its own is not repeated within a few seconds either.
    await status.refreshPCStatus();
    expect(sessionsCalls()).toHaveLength(1);
  });

  test('waiting leaves out asks; errors say why; a stopped wait is not an error', async () => {
    expect(
      status
        .waitingOf([
          session({ id: '1', activity: 'blocked' }),
          session({ id: '2', activity: 'blocked', badges: ['ask'] }),
          session({ id: '3', activity: 'busy' }),
          session({ id: '4' }),
        ])
        .map((x) => x.id),
    ).toEqual(['1']);

    const pc = await pairedPC();
    await reloadPCs();
    const { result } = await renderHook(() => status.usePCStatus());
    expect(result.current).toBeNull();
    await act(async () => status.publishError(pc.id, new CancelledError()));
    expect(result.current).toBeNull();
    await act(async () => status.publishError(pc.id, new NetworkError(undefined, true)));
    expect(result.current).toMatchObject({ pcId: pc.id, state: 'error', cause: 'pc_unreachable', waiting: [] });
    await act(async () => status.publishSessions(pc.id, [session({ id: 'w', activity: 'blocked' })]));
    expect(result.current).toMatchObject({ state: 'ok', waiting: [{ id: 'w' }] });
  });
});
