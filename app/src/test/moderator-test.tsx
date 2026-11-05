// C6 (#19, 2026-11-05, the user's decision): no judging. The moderator AI
// alone writes the combined answer from the answers, for two AIs as for
// three; the screen shows that answer, the AIs' own answers and no cards,
// 'AI 답이 다른 곳' or judging details (features.judging off, the fake
// agent's noJudging as api.Options.NoJudging). Every user event is awaited
// (R-07).

import { act, userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { features } from '@/ask/features';
import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

beforeEach(() => {
  features.judging = false;
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableJudge();
  agent.enableSynth();
  agent.noJudging = true;
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  if (Object.keys(prefs).length) await saveAskPrefs({ ...defaultAskPrefs, ...prefs });
  await renderRouter(appDir, { initialUrl: '/' });
  return user;
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

function noJudgingOnScreen() {
  for (const name of [/^Differ: /, /^Check this: /, /^Agree: /, /^Only one: /]) expect(screen.queryByRole('button', { name })).toBeNull();
  expect(screen.queryByText('Evidence')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Judging details' })).toBeNull();
  expect(screen.queryByText(/^Where the AI answers differ/)).toBeNull();
  expect(screen.queryByText(/^Comparing/)).toBeNull();
  expect(screen.queryByText('Written from the answers alone, without comparison results.')).toBeNull();
}

test('three AIs: the moderator writes the combined answer, with no cards or judging details', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  expect(await screen.findByText('Once the answers are in, the moderator AI reads them all and writes a combined answer. Its company receives the question and every answer.')).toBeTruthy();
  await ask(user, 'Tell me about Seoul');
  expect(await screen.findByText('Once every answer is in, the moderator AI writes a combined answer.')).toBeTruthy();

  // One step only: writing the combined answer.
  expect(await screen.findByLabelText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByLabelText(/^Next step/)).toBeNull();
  noJudgingOnScreen();

  agent.synthReply = 'ok';
  expect(await written()).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(screen.getByText('answers 1 s · combined in 15 s')).toBeTruthy();
  expect(screen.getByText('An AI wrote this combined answer, so it can be wrong.')).toBeTruthy();
  noJudgingOnScreen();
  // The AIs' own answers are there as cards.
  for (const name of ['Claude', 'Codex', 'OpenCode']) expect(screen.getAllByLabelText(new RegExp(`^${name}'s answer`)).length).toBeGreaterThan(0);
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'], synthesis: { agent: 'claude' } });
  expect(agent.synthCalls).toBe(1);

  // Done: the polling stops.
  const gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);
});

test('two AIs: a combined answer too (#19), where there was no comparison before', async () => {
  const user = await open({ off: ['opencode'] });
  await ask(user, 'Tell me about Seoul');
  expect(await written()).toBeTruthy();
  expect(screen.queryByText(/^Only two AIs were asked/)).toBeNull();
  noJudgingOnScreen();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex'], synthesis: { agent: 'claude' } });
  expect(agent.comparisonCount()).toBe(1);
  expect(agent.synthCalls).toBe(1);
});

test('asking one AI: no combined answer, its answer in full', async () => {
  const user = await open({ off: ['codex', 'opencode'] });
  await ask(user, 'q');
  expect(await screen.findByText(answers.claude)).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
  expect(agent.comparisonCount()).toBe(0);
});

test('the combined answer switched off: compared as the answers come, written on a press', async () => {
  const user = await open({ synth: false, compare: false });
  await ask(user, 'Tell me about Seoul');
  const write = await screen.findByRole('button', { name: 'Write a combined answer' }, { timeout: 5000 });
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'], synthesis: { off: true } });
  expect(agent.synthCalls).toBe(0);
  noJudgingOnScreen();
  await user.press(write);
  expect(await written()).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
});

test('Settings: one switch for the combined answer, and no word of judging', async () => {
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/settings' });
  expect(await screen.findByLabelText('Write a combined answer once the answers are in')).toBeTruthy();
  expect(screen.queryByLabelText('Compare answers automatically')).toBeNull();
  expect(screen.queryByLabelText('Write a combined answer once compared')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'About the moderator' }));
  expect(screen.queryByText(/judg/i)).toBeNull();
});
