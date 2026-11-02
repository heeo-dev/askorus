// S5c: the AIs' own answers around a combined answer on 한 번에 (F-Result-
// Expanded, F-Followup-One, B2-Many-Sheet): the cards of each AI's answer
// and the answer sheet they open (copy, only this AI, continue in the Claude
// app), a follow-up to one AI from there, and the sheet of the AIs that
// answered with how each one did. Every user event is awaited (R-07).

import { userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'Seoul is the capital. It has **9.4 million** people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableJudge();
  agent.enableSynth();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function open() {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  await renderRouter(appDir, { initialUrl: '/' });
  return user;
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const card = (name: string) => screen.getByRole('button', { name: new RegExp(`^${name}'s answer\\.`) });
const close = () => screen.getByRole('button', { name: 'Close' });

test('each AI’s answer is a card with its model and the start of its answer, in plain text', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  expect(screen.getByRole('header', { name: 'Answers by AI (3)' })).toBeTruthy(); // not 'AI 3' (UX-10)
  expect(card('Claude').props.accessibilityLabel).toBe("Claude's answer. claude-standard. Seoul is the capital. It has 9.4 million people. Try the palace.");
  expect(within(card('Claude')).getByText('Seoul is the capital. It has 9.4 million people. Try the palace.')).toBeTruthy(); // no ** marks
  expect(within(card('Codex')).getByText('codex-standard')).toBeTruthy();
  expect(card('Codex')).not.toBeSelected();
});

test('a card opens the whole answer: who judged it, copy, only this AI, and continue in the Claude app', async () => {
  const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = await open();
  await ask(user, 'q');
  await written();

  await user.press(card('Claude'));
  const sheet = within(await screen.findByTestId('answer-sheet'));
  expect(sheet.getByRole('header', { name: 'Claude' })).toBeTruthy();
  expect(sheet.getByText('claude-standard · 1.2 s')).toBeTruthy();
  expect(sheet.getByText('My PC · Judged by: Codex, OpenCode')).toBeTruthy();
  expect(sheet.getByText('9.4 million')).toHaveStyle({ fontWeight: '700' }); // drawn from its Markdown
  await user.press(sheet.getByRole('button', { name: 'Copy' }));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(answers.claude);
  expect(await sheet.findByText('Copied.')).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Continue in the Claude app' }));
  await waitFor(() => expect(openURL).toHaveBeenCalledWith(agent.remoteUrl));
  expect(agent.callsTo('POST', `/v1/sessions/${encodeURIComponent('claude:s-1')}/open`)).toHaveLength(1);
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('answer-sheet')).toBeNull());

  // Only Claude carries on in the Claude app.
  await user.press(card('Codex'));
  const codex = within(await screen.findByTestId('answer-sheet'));
  expect(codex.getByText(answers.codex)).toBeTruthy();
  expect(codex.queryByRole('button', { name: 'Continue in the Claude app' })).toBeNull();
  expect(codex.getByRole('button', { name: 'Only this AI' })).toBeTruthy();
});

test('only this AI: its card is marked, the chip and the box name it, and it is asked alone', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(card('Codex'));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  await waitFor(() => expect(screen.queryByTestId('answer-sheet')).toBeNull());

  expect(card('Codex')).toBeSelected();
  expect(card('Codex').props.accessibilityLabel).toMatch(/^Codex's answer\. Only this AI\. /);
  expect(within(card('Codex')).getByText('Only this AI')).toBeTruthy();
  expect(card('Claude')).not.toBeSelected();
  expect(within(screen.getByTestId('follow-bar')).getByText('Only Codex')).toBeTruthy();
  expect(screen.getByLabelText('Ask Codex a follow-up')).toBeTruthy();

  // Cancelled: everyone again.
  await user.press(screen.getByRole('button', { name: 'Stop asking only Codex' }));
  expect(card('Codex')).not.toBeSelected();
  expect(screen.getByLabelText('Follow up')).toBeTruthy();

  await user.press(card('Codex'));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  agent.askReplies = {};
  await user.type(screen.getByLabelText('Ask Codex a follow-up'), 'only you');
  await user.press(screen.getByRole('button', { name: 'Send to Codex' }));
  expect(await screen.findByText('codex says: only you')).toBeTruthy();
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);
  expect(agent.runRequests()[1].compare).toBeUndefined();
  expect(card('Codex')).not.toBeSelected();
});

test('the AIs that answered: a sheet with each AI’s model and time, a failed one with why, each opening its answer', async () => {
  agent.askReplies.opencode = { code: 'ask_failed', message: 'not logged in' };
  agent.askAgents = [...agent.askAgents, { agent: 'kimi', version: '1.0', available: true, capabilities: ['ask'], ask: { tiers: { standard: { model: 'k2' } } } }];
  agent.askReplies.kimi = 'Seoul is the capital.';
  const user = await open();
  await ask(user, 'q');
  await written();
  // Three of the four asked answered: the pile shows them, the card of the failed one says why.
  const pile = screen.getByRole('button', { name: 'AIs that answered: 3 (Claude, Codex, kimi). Details' });
  expect(card('OpenCode').props.accessibilityLabel).toBe("OpenCode's answer. opencode-standard. The AI reported an error.");

  await user.press(pile);
  const sheet = within(await screen.findByTestId('ai-sheet'));
  expect(sheet.getByRole('header', { name: '4 AIs' })).toBeTruthy();
  expect(sheet.getByText('answers 1 s · compared in 10 s')).toBeTruthy();
  const row = (label: RegExp) => sheet.getByRole('button', { name: label });
  expect(row(/^Claude, claude-standard$/)).toHaveAccessibilityValue({ text: 'Answered, 1 s' });
  expect(row(/^OpenCode, opencode-standard, Failed · The AI reported an error\.$/)).toHaveAccessibilityValue({ text: 'Did not answer' });
  expect(sheet.getByText('Failed · The AI reported an error.')).toBeTruthy();

  await user.press(row(/^OpenCode/));
  const answer = within(await screen.findByTestId('answer-sheet'));
  expect(answer.getByText('The AI reported an error.\nnot logged in')).toBeTruthy();
  expect(answer.queryByRole('button', { name: 'Copy' })).toBeNull();
});

test('in Korean the cards and sheets read in Korean', async () => {
  setLang('ko');
  const user = await open();
  await user.type(await screen.findByLabelText('질문을 적으세요'), 'q');
  await user.press(screen.getByRole('button', { name: '묻기' }));
  await written();
  await user.press(screen.getByRole('button', { name: '답한 AI 3개: Claude, Codex, OpenCode. 자세히' }));
  const sheet = within(await screen.findByTestId('ai-sheet'));
  expect(sheet.getByRole('header', { name: 'AI 3개' })).toBeTruthy();
  expect(sheet.getByRole('button', { name: 'Claude, claude-standard' })).toHaveAccessibilityValue({ text: '답함, 1초' });
  await user.press(sheet.getByRole('button', { name: 'Codex, codex-standard' }));
  const answer = within(await screen.findByTestId('answer-sheet'));
  expect(answer.getByText('codex-standard · 1.2초')).toBeTruthy();
  expect(answer.getByText('내 PC · 판정: Claude, OpenCode')).toBeTruthy();
  await user.press(answer.getByRole('button', { name: '이 AI에게만' }));
  expect(await screen.findByLabelText('Codex에게 이어서 묻기')).toBeTruthy();
  expect(within(screen.getByRole('button', { name: /^Codex의 답\./ })).getByText('이 AI에게만')).toBeTruthy();
});
