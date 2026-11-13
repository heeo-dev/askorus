// #53: one phone with several PCs. A Windows PC and the Linux inside it
// call themselves the same, so the screens show what tells them apart; the
// ask screen says which PC answers, and says so when opening a conversation
// moved the app to that conversation's PC. Every user event is awaited.

import { act, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { conversations } from '@/ask/conversations';
import { loadAskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { unpairPC } from '@/ui/PCSheet';
import { reloadPCs } from '@/ui/shell/pcs';

const appDir = './src/app';
const wslHost = 'desktop-wsl.tail0.ts.net';
const winLabel = 'DESKTOP-TEST (Windows · desktop-test)';
const wslLabel = 'DESKTOP-TEST (Linux · desktop-wsl)';
const switched = `Switched to the PC this conversation was asked on: ${winLabel}`;
type User = ReturnType<typeof userEvent.setup>;

let win: FakeAgent;
let wsl: FakeAgent;
// The Linux PC cannot be reached (off, or its agent stopped): the request fails.
let wslDown = false;

beforeEach(() => {
  win = new FakeAgent();
  win.enableAsk();
  wsl = new FakeAgent();
  wsl.enableAsk();
  wsl.os = 'linux';
  wslDown = false;
  globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(u));
    if (url.hostname === wslHost) {
      if (wslDown) throw new TypeError('Network request failed');
      return wsl.fetch(`http://${HOST}:${PORT}${url.pathname}${url.search}`, init);
    }
    return win.fetch(u, init);
  }) as typeof fetch;
});

afterEach(() => jest.restoreAllMocks());

// Both PCs carry the name their host gives them: the same one.
// `os` as pairing keeps it; left out, the app learns it the first time the PC answers.
const paired = (host = HOST, os?: string) =>
  new ConnectionStore().add(
    { kind: 'pc', name: 'DESKTOP-TEST', host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x', ...(os ? { os } : {}) },
    TOKEN,
  );

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

test('with one PC the ask screen shows no PC line', async () => {
  await paired();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByRole('header', { name: 'What would you like to ask?' })).toBeTruthy();
  expect(screen.queryByTestId('pc-line')).toBeNull();
});

test('with two PCs of one name the ask screen says which answers, and opening a conversation of the other says the PC changed', async () => {
  const user = userEvent.setup();
  const desktop = await paired();
  await paired(wslHost);
  await renderRouter(appDir, { initialUrl: '/' });

  // The first paired PC answers; the line tells it from the other by system and address.
  expect(await screen.findByRole('button', { name: `My PC: ${winLabel}, Pick another PC` })).toBeTruthy();
  await ask(user, 'on windows');
  await screen.findByText('opencode says: on windows');
  expect(win.runRequests()).toHaveLength(1);

  // The line opens the picker. Picking by hand starts a new conversation there and announces nothing.
  await user.press(screen.getByTestId('pc-line'));
  const picker = within(await screen.findByTestId('pc-picker'));
  expect(await picker.findByRole('button', { name: `DESKTOP-TEST, Windows · ${HOST}, Connected · agent 0.1.0` })).toBeSelected();
  await user.press(await picker.findByRole('button', { name: `DESKTOP-TEST, Linux · ${wslHost}, Connected · agent 0.1.0` }));
  expect(await screen.findByRole('button', { name: `My PC: ${wslLabel}, Pick another PC` })).toBeTruthy();
  expect(screen.queryByText(switched)).toBeNull();
  await ask(user, 'on linux');
  await screen.findByText('opencode says: on linux');
  expect(wsl.runRequests()).toHaveLength(1);
  expect(win.runRequests()).toHaveLength(1);

  // The drawer names the current PC the same way.
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  expect(await screen.findByRole('button', { name: 'DESKTOP-TEST, Linux · desktop-wsl, Connected' })).toBeTruthy();

  // Opening the Windows conversation moves the app to that PC, and the line says so.
  await user.press(screen.getByRole('button', { name: 'on windows' }));
  expect(await screen.findByText('opencode says: on windows')).toBeTruthy();
  expect(await screen.findByText(switched)).toBeTruthy();
  expect(screen.getByRole('button', { name: `${switched}, Pick another PC` })).toBeTruthy();
  await waitFor(async () => expect((await loadAskPrefs()).pc).toBe(desktop.id));
  expect(conversations.current()?.title).toBe('on windows');

  // After a few seconds the line is the plain PC again.
  await waitFor(() => expect(screen.queryByText(switched)).toBeNull(), { timeout: 8000 });
  expect(screen.getByRole('button', { name: `My PC: ${winLabel}, Pick another PC` })).toBeTruthy();
});

test('a PC is renamed on this phone from its sheet in Settings', async () => {
  const user = userEvent.setup();
  await paired();
  const linux = await paired(wslHost);
  const r = await renderRouter(appDir, { initialUrl: '/settings' });

  // Short rows: the system, and the first part of the address while the name is shared.
  expect(await screen.findByRole('button', { name: 'DESKTOP-TEST, Windows · desktop-test, Connected' })).toBeTruthy();
  await user.press(await screen.findByRole('button', { name: 'DESKTOP-TEST, Linux · desktop-wsl, Connected' }));
  const sheet = within(await screen.findByTestId('pc-sheet'));
  expect(sheet.getByText(`Linux · ${wslHost}:${PORT}`)).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Rename' }));
  await waitFor(() => expect(r.getPathname()).toBe(`/pc/${linux.id}/rename`));

  const field = await screen.findByLabelText('Name on this phone');
  await waitFor(() => expect(field.props.value).toBe('DESKTOP-TEST'));
  expect(screen.getByText('The name is kept on this phone only. The PC itself is not renamed.')).toBeTruthy();
  // A name of spaces only cannot be saved.
  await user.clear(field);
  await user.type(field, '   ');
  expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  await user.clear(field);
  await user.type(field, '  WSL ');
  await user.press(screen.getByRole('button', { name: 'Save' }));

  // Back in Settings: the names differ now, so the address is not needed.
  await waitFor(() => expect(r.getPathname()).toBe('/settings'));
  expect(await screen.findByRole('button', { name: 'WSL, Linux, Connected' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'DESKTOP-TEST, Windows, Connected' })).toBeTruthy();
  expect(await new ConnectionStore().get(linux.id)).toMatchObject({ name: 'WSL', named: true, os: 'linux' });
});

// The line under the header leads to the picker; picking there is by hand.
async function pick(user: User, host: string, system: string) {
  await user.press(screen.getByTestId('pc-line'));
  const picker = within(await screen.findByTestId('pc-picker'));
  await user.press(await picker.findByRole('button', { name: new RegExp(`^DESKTOP-TEST, ${system} · ${host.replace(/\./g, '\\.')}, `) }));
}

const waiting = 'Waiting: it goes once the question before it is done.';

test('a question waits only for a conversation still running on the same PC', async () => {
  const user = userEvent.setup();
  await paired();
  await paired(wslHost);
  await renderRouter(appDir, { initialUrl: '/' });
  await screen.findByRole('button', { name: `My PC: ${winLabel}, Pick another PC` });

  // The Linux PC goes on answering while the phone moves to the Windows PC.
  await pick(user, wslHost, 'Linux');
  await screen.findByRole('button', { name: `My PC: ${wslLabel}, Pick another PC` });
  wsl.runLimit = 1;
  await ask(user, 'slow on linux');
  await screen.findAllByText('Answering…');
  await pick(user, HOST, 'Windows');
  expect(await screen.findByText('The question before is still being answered and is kept in the history.')).toBeTruthy();

  // The Windows PC answers at once: it has nothing to wait for.
  await ask(user, 'on windows');
  await screen.findByText('opencode says: on windows');
  expect(screen.queryByText(waiting)).toBeNull();
  expect(win.runRequests()).toHaveLength(1);
  expect(wsl.runRequests()).toHaveLength(1);

  // Back on the Linux PC a new question does wait, until the one before is done.
  await pick(user, wslHost, 'Linux');
  await screen.findByRole('button', { name: `My PC: ${wslLabel}, Pick another PC` });
  await ask(user, 'next on linux');
  expect(await screen.findByText(waiting)).toBeTruthy();
  expect(wsl.runRequests()).toHaveLength(1);
  wsl.runLimit = Infinity;
  await waitFor(() => expect(wsl.runRequests()).toHaveLength(2), { timeout: 8000 });
  expect(wsl.runRequests()[1].prompt).toBe('next on linux');
  await screen.findByText('opencode says: next on linux');
  expect(win.runRequests()).toHaveLength(1);
});

test('a PC that does not answer is named, and another can be picked from the card', async () => {
  const user = userEvent.setup();
  await paired();
  await paired(wslHost);
  await renderRouter(appDir, { initialUrl: '/' });
  await screen.findByRole('button', { name: `My PC: ${winLabel}, Pick another PC` });
  await pick(user, wslHost, 'Linux');
  await screen.findByRole('button', { name: `My PC: ${wslLabel}, Pick another PC` });
  await ask(user, 'on linux');
  await screen.findByText('opencode says: on linux');
  await pick(user, HOST, 'Windows');
  await screen.findByRole('header', { name: 'What would you like to ask?' });

  // The Linux PC's agent stops; its conversation is opened from the drawer.
  wslDown = true;
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(await within(await screen.findByTestId('drawer')).findByRole('button', { name: 'on linux' }));
  expect(await screen.findByText("Can't reach the PC")).toBeTruthy();
  // Which PC, and the way to look at and start the agent: the same on every system since #52.
  expect(screen.getByText(`My PC: ${wslLabel}`)).toBeTruthy();
  expect(screen.getByText(/on the PC: askorus-agent status; if it is off, askorus-agent start/)).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  // The conversation is still there to read.
  expect(screen.getByText('opencode says: on linux')).toBeTruthy();

  // The card leads to the picker, and from there back to a PC that answers.
  await user.press(screen.getByRole('button', { name: 'Pick another PC' }));
  const picker = within(await screen.findByTestId('pc-picker'));
  await user.press(await picker.findByRole('button', { name: new RegExp(`^DESKTOP-TEST, Windows · `) }));
  expect(await screen.findByRole('header', { name: 'What would you like to ask?' })).toBeTruthy();
  expect(screen.queryByText('The PC is on, but the agent is not running')).toBeNull();
});

test('the conversation of an unpaired PC can only be read; pairing that PC again carries it on', async () => {
  const user = userEvent.setup();
  await paired();
  const linux = await paired(wslHost);
  await renderRouter(appDir, { initialUrl: '/' });
  await screen.findByRole('button', { name: `My PC: ${winLabel}, Pick another PC` });
  await pick(user, wslHost, 'Linux');
  await screen.findByRole('button', { name: `My PC: ${wslLabel}, Pick another PC` });
  await ask(user, 'on linux');
  await screen.findByText('opencode says: on linux');
  expect(screen.getByLabelText('Follow up')).toBeTruthy();

  // The Linux PC is unpaired (Settings does this): the Windows PC is the only one left.
  await act(async () => {
    await unpairPC(linux);
    await reloadPCs();
  });
  expect((await loadAskPrefs()).pc).toBe('');
  expect(await screen.findByRole('header', { name: 'What would you like to ask?' })).toBeTruthy();
  expect(screen.queryByTestId('pc-line')).toBeNull();

  // Its conversation opens, to read: no follow-up, nothing sent to the Windows PC.
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(await within(await screen.findByTestId('drawer')).findByRole('button', { name: 'on linux' }));
  expect(await screen.findByText('opencode says: on linux')).toBeTruthy();
  expect(
    await screen.findByText('The PC this conversation was asked on is no longer paired, so it can only be read. Pair with that PC again to carry it on.'),
  ).toBeTruthy();
  expect(screen.queryByLabelText('Follow up')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Only this AI' })).toBeNull();
  expect(win.runRequests()).toHaveLength(0);

  // Paired again at the same address, the PC takes its id back: the conversation is its own again.
  let again = linux;
  await act(async () => {
    wsl.tokenValid = true; // the PC allowed the phone again
    again = await paired(wslHost, 'linux');
    await reloadPCs();
  });
  expect(again.id).toBe(linux.id);
  // It does not become the current PC on its own (R-94): the conversation says where it belongs.
  expect(await screen.findByText(`This conversation was asked on another PC: ${wslLabel}. Switch to it to carry it on.`)).toBeTruthy();
  expect(screen.queryByLabelText('Follow up')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Switch to that PC' }));
  await user.type(await screen.findByLabelText('Follow up'), 'and then?');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await screen.findByText('opencode says: and then?');
  // The follow-up went to the Linux PC, in the sessions of the first question.
  expect(wsl.runRequests()).toHaveLength(2);
  expect(wsl.runRequests()[1].agents[0]).toMatchObject({ agent: 'claude', session: 'claude:s-1' });
  expect(win.runRequests()).toHaveLength(0);
}, 60000);
