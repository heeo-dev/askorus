// Manage AIs (#20 decision 7): the current PC's AIs with version, sign-in
// and usage, a switch each that keeps the AI out of the AIs that answer
// and of the moderators on this phone, a sheet of what it does and where
// its questions go, and the AIs the PC does not have with the install
// guide. Every user event is awaited (R-07).

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import type { AdapterInfo } from '@/api/types';
import { defaultAskPrefs, loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';

const agy: AdapterInfo = {
  agent: 'agy',
  version: '1.5.11',
  available: true,
  capabilities: ['ask', 'synthesize'],
  ask: {
    tiers: { quick: { model: 'gemini-3.8-flash', effort: 'low' }, standard: { model: 'gemini-3.8-flash', effort: 'medium' }, deep: { model: 'gemini-3.8-flash', effort: 'high' } },
    images: { all: true },
  },
  usage: [
    { name: 'Gemini Models', left: 0.81, resetAt: '2026-11-25T07:21:31Z' },
    { name: 'Claude and GPT models', left: 1, resetAt: '2026-11-25T08:47:59Z' },
  ],
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.askAgents = [...agent.askAgents, agy];
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function open(url = '/ais') {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  await renderRouter(appDir, { initialUrl: url });
  return user;
}

test('Settings opens Manage AIs from the Asking section', async () => {
  const user = await open('/settings');
  await user.press(await screen.findByText('Manage AIs'));
  expect(await screen.findByText('AIs on DESKTOP-TEST')).toBeTruthy();
});

test("the PC's AIs with version, sign-in and usage, and a switch each", async () => {
  agent.askAgents = agent.askAgents.map((a) => (a.agent === 'codex' ? { ...a, notices: ['codex_signed_out'] } : a));
  const user = await open();
  expect(await screen.findByText('AIs on DESKTOP-TEST')).toBeTruthy();
  for (const name of ['Claude', 'Codex', 'OpenCode', 'Antigravity']) expect(screen.getByText(name)).toBeTruthy();
  expect(screen.getByText('1.5.11 · signed in · 81% left this week')).toBeTruthy();
  expect(screen.getByText('0.164.0 · not signed in')).toBeTruthy();
  expect(screen.queryByText('Not on this PC')).toBeNull();

  const sw = screen.getByRole('switch', { name: 'Use Antigravity for questions' });
  expect(sw).toBeChecked();
  await user.press(sw);
  await waitFor(() => expect(screen.getByRole('switch', { name: 'Use Antigravity for questions' })).not.toBeChecked());
  expect((await loadAskPrefs()).off).toEqual(['agy']);
  // The switch did not open the sheet.
  expect(screen.queryByTestId('ai-sheet')).toBeNull();
  await user.press(screen.getByRole('switch', { name: 'Use Antigravity for questions' }));
  await waitFor(async () => expect((await loadAskPrefs()).off).toEqual([]));
});

test("a row's sheet: usage, tier models, what it does and where questions go", async () => {
  const opened = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = await open();
  await user.press(await screen.findByText('Antigravity'));
  const sheet = await screen.findByTestId('ai-sheet');
  const s = within(sheet);
  expect(s.getByText('Gemini Models')).toBeTruthy();
  expect(s.getAllByText('81% left this week').length).toBe(1);
  expect(s.getByText('gemini-3.8-flash · low')).toBeTruthy();
  expect(s.getByText('gemini-3.8-flash · high')).toBeTruthy();
  for (const f of ['Ask', 'Follow-up', 'Moderator', 'Debate', 'Photos']) expect(s.getByText(f)).toBeTruthy();
  expect(s.getByText('Questions and answers go to Google.')).toBeTruthy();
  await user.press(s.getByRole('button', { name: 'Open the AI install guide' }));
  expect(opened).toHaveBeenCalledWith('https://github.com/heeo-dev/askorus#installing-the-ai-programs');
});

test('an AI without photos or moderating says so in its sheet', async () => {
  const user = await open();
  await user.press(await screen.findByText('OpenCode'));
  const s = within(await screen.findByTestId('ai-sheet'));
  expect(s.getByText('No photos')).toBeTruthy();
  expect(s.getByText('Not a moderator')).toBeTruthy();
});

test('the AIs the PC does not have come with the install guide', async () => {
  agent.askAgents = agent.askAgents.filter((a) => a.agent !== 'opencode' && a.agent !== 'agy');
  const opened = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = await open();
  expect(await screen.findByText('Not on this PC')).toBeTruthy();
  expect(screen.getAllByText('Not installed').length).toBe(2);
  await user.press(screen.getByText('Antigravity'));
  expect(opened).toHaveBeenCalledWith('https://github.com/heeo-dev/askorus#installing-the-ai-programs');
});

test('an AI turned off leaves the moderators in Settings', async () => {
  const user = await open('/settings');
  expect(await screen.findByRole('radio', { name: 'Antigravity' })).toBeTruthy();
  await user.press(screen.getByText('Manage AIs'));
  await user.press(await screen.findByRole('switch', { name: 'Use Antigravity for questions' }));
  await waitFor(async () => expect((await loadAskPrefs()).off).toEqual(['agy']));
  await user.press(screen.getByRole('button', { name: 'Back' }));
  await waitFor(() => expect(screen.queryByRole('radio', { name: 'Antigravity' })).toBeNull());
  expect(screen.getByRole('radio', { name: 'Claude' })).toBeTruthy();
});

test('in Korean', async () => {
  setLang('ko');
  await open();
  expect(await screen.findByText('DESKTOP-TEST의 AI')).toBeTruthy();
  expect(screen.getByText('1.5.11 · 로그인됨 · 이번 주 81% 남음')).toBeTruthy();
  expect(screen.getByRole('switch', { name: '묻기에 쓰기: Antigravity' })).toBeChecked();
});

test('turning the moderator off picks another moderator (R-128)', async () => {
  await saveAskPrefs({ ...defaultAskPrefs, synthAgent: 'agy' });
  const user = await open('/settings');
  expect(await screen.findByRole('radio', { name: 'Antigravity' })).toBeChecked();
  await user.press(screen.getByText('Manage AIs'));
  await user.press(await screen.findByRole('switch', { name: 'Use Antigravity for questions' }));
  await waitFor(async () => expect(await loadAskPrefs()).toMatchObject({ off: ['agy'], synthAgent: 'claude' }));
  await user.press(screen.getByRole('button', { name: 'Back' }));
  await waitFor(() => expect(screen.queryByRole('radio', { name: 'Antigravity' })).toBeNull());
  expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked();
});

test('several PCs: one row shows the PC and opens the PC picker', async () => {
  await new ConnectionStore().add({ kind: 'pc', name: 'LAPTOP-TEST', host: 'laptop-test.tail0.ts.net', port: PORT, deviceId: 'dev-2', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  const user = await open();
  const row = await screen.findByRole('button', { name: 'PC' });
  await user.press(row);
  expect(await screen.findByTestId('pc-picker')).toBeTruthy();
});
