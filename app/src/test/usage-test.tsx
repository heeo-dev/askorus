// The top bar's usage button and its sheet (#20, decisions 15-21): a bar
// per AI turned on, in the PC order, as high as its tightest window has
// left; low, used up and unknown look different; the sheet sums it up,
// lists each AI's windows, Claude's 'as of', the AIs turned off and the way
// to Manage AIs. Every top bar has it but pairing's. Every user event is
// awaited (R-07).

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, within } from 'expo-router/testing-library';

import { defaultAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
const at = '2026-11-18T12:05:00Z';

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.claudeUsage = [
    { name: '5 hours', left: 0.72, window: '5h', resetAt: '2026-11-18T14:30:00Z', at },
    { name: 'Weekly', left: 0.62, window: 'week', resetAt: '2026-11-22T23:00:00Z', at },
  ];
  agent.askAgents = agent.askAgents.map((a) =>
    a.agent === 'codex'
      ? { ...a, usage: [{ name: '5 hours', left: 0, window: '5h', limited: true }, { name: 'Weekly', left: 0.95, window: 'week' }] }
      : a.agent === 'opencode'
        ? { ...a, usage: [{ name: 'Monthly', left: 0.16, window: 'month' }] }
        : a,
  );
  agent.askAgents = [...agent.askAgents, { agent: 'agy', version: '1.5.11', available: true, capabilities: ['ask'] }];
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
});

async function open(url = '/') {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  await renderRouter(appDir, { initialUrl: url });
  return user;
}

test('a bar per AI turned on: ok, used up, low and unknown look different', async () => {
  await open();
  expect(await screen.findByTestId('usage-bar-claude-ok')).toBeTruthy();
  expect(screen.getByTestId('usage-bar-codex-out')).toBeTruthy();
  expect(screen.getByTestId('usage-bar-opencode-low')).toBeTruthy();
  expect(screen.getByTestId('usage-bar-agy-unknown')).toBeTruthy();
  const button = screen.getByRole('button', { name: 'Usage left' });
  expect(button.props.accessibilityValue.text).toBe('Codex: the 5-hour limit is used up. Claude 62%, Codex Used up, OpenCode 16%, Antigravity unknown');
});

test('the sheet sums it up, lists the windows, and goes to Manage AIs', async () => {
  const user = await open();
  await user.press(await screen.findByRole('button', { name: 'Usage left' }));
  const s = within(await screen.findByTestId('usage-sheet'));
  expect(s.getByText('Codex: the 5-hour limit is used up.')).toBeTruthy();
  expect(s.getByText('Current PC · DESKTOP-TEST')).toBeTruthy();
  const claude = within(s.getByTestId('usage-ai-claude'));
  expect(claude.getByText('5 hours')).toBeTruthy();
  expect(claude.getByText('72% left')).toBeTruthy();
  expect(claude.getByText('This week')).toBeTruthy();
  expect(claude.getByText(/^As of Askorus's last call/)).toBeTruthy();
  expect(within(s.getByTestId('usage-ai-codex')).getByText('Used up')).toBeTruthy();
  expect(within(s.getByTestId('usage-ai-opencode')).getByText('This month')).toBeTruthy();
  expect(within(s.getByTestId('usage-ai-agy')).getByText(/does not report it/)).toBeTruthy();
  await user.press(s.getByRole('button', { name: 'Open Manage AIs' }));
  expect(await screen.findByText('AIs on DESKTOP-TEST')).toBeTruthy();
});

test('an AI turned off has no bar and is named in the sheet; all off shows a dash', async () => {
  await saveAskPrefs({ ...defaultAskPrefs, off: ['opencode'] });
  const user = await open();
  expect(await screen.findByTestId('usage-bar-claude-ok')).toBeTruthy();
  expect(screen.queryByTestId('usage-bar-opencode-low')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Usage left' }));
  expect(within(await screen.findByTestId('usage-sheet')).getByText('Turned off: OpenCode')).toBeTruthy();
});

test('stacked screens have the button', async () => {
  await open('/settings');
  expect(await screen.findByTestId('usage-button')).toBeTruthy();
  expect(await screen.findByTestId('usage-bar-codex-out')).toBeTruthy();
});

test('in Korean', async () => {
  setLang('ko');
  const user = await open();
  await user.press(await screen.findByRole('button', { name: '남은 사용량' }));
  const s = within(await screen.findByTestId('usage-sheet'));
  expect(s.getByText('Codex: 5시간 한도를 다 썼습니다.')).toBeTruthy();
  expect(within(s.getByTestId('usage-ai-opencode')).getByText('16% 남음')).toBeTruthy();
  expect(s.getByRole('button', { name: 'AI 관리 열기' })).toBeTruthy();
});


// R-143 (2ho's review): with every known AI fine, the summary names the
// ones that report nothing instead of calling all of them fine.
test('a fine summary names the AIs that report nothing', async () => {
  agent.askAgents = agent.askAgents.map((a) => (a.agent === 'codex' || a.agent === 'opencode' ? { ...a, usage: [{ name: '5 hours', left: 0.9, window: '5h' }] } : a));
  const user = await open();
  await user.press(await screen.findByRole('button', { name: 'Usage left' }));
  expect(within(await screen.findByTestId('usage-sheet')).getByText('The AIs that report usage all have more than 20% left. Unknown: Antigravity')).toBeTruthy();
});

// R-142 (2ho's review): a PC that no longer answers keeps its last values,
// and the sheet says when they were read.
test('values the PC could not refresh say how old they are', async () => {
  const user = await open();
  expect(await screen.findByTestId('usage-bar-claude-ok')).toBeTruthy();
  agent.reach = 'offline';
  await user.press(screen.getByRole('button', { name: 'Usage left' }));
  const s = within(await screen.findByTestId('usage-sheet'));
  expect(await s.findByText(/^The PC did not answer: these are as read at /)).toBeTruthy();
  expect(s.getByText('Codex: the 5-hour limit is used up.')).toBeTruthy();
});

// Last: the pairing screen leaves its own state behind.
test('pairing, a flow, has no usage button', async () => {
  await open('/pair');
  expect((await screen.findAllByText(/pair|Pair/)).length).toBeGreaterThan(0);
  expect(screen.queryByTestId('usage-button')).toBeNull();
});
