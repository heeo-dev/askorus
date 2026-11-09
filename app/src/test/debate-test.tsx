// P2-5 토론 (#26): the debate under the combined answer, against a pretend
// agent that runs debates (fakeAgent enableDebate: each poll moves the
// debate one step). The button only where the agent runs debates, the
// notice the first time, the chat room with its chips, the answer after
// the debate, stop, no point, failures, a kept conversation opened again,
// and Korean. Every user event is awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { conversations } from '@/ask/conversations';
import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import type { Turn } from '@/ask/thread';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { DEBATE_AFTER, FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'The first ferry leaves at 7 a.m.',
  codex: 'The first ferry leaves at 8 a.m.',
  opencode: 'It leaves at 8 a.m. from pier 2.',
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.enableDebate();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

const pairedPC = () =>
  new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await pairedPC();
  if (Object.keys(prefs).length) await saveAskPrefs({ ...defaultAskPrefs, ...prefs });
  await renderRouter(appDir, { initialUrl: '/' });
  return user;
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const after = () => screen.findByText(DEBATE_AFTER, {}, { timeout: 8000 });
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('no Debate button when the agent runs no debates', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  await ask(user, 'When is the first ferry?');
  await written();
  await later();
  expect(screen.queryByRole('button', { name: 'Debate' })).toBeNull();
});

test('the first debate: the notice, the chat room with its chips, and the answer after it', async () => {
  const user = await open();
  await ask(user, 'When is the first ferry?');
  await written();
  expect(screen.getByText('Uses the AIs about 10 more times · 2–3 min')).toBeTruthy();

  // The first time, what a debate sends where; cancel leaves it as it was.
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(screen.getByRole('header', { name: 'Before the debate' })).toBeTruthy();
  expect(screen.getByText(/the company of every AI that answered gets the other AIs' answers/)).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('header', { name: 'Before the debate' })).toBeNull();
  expect(agent.debateRequests()).toHaveLength(0);

  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(screen.getByRole('header', { name: 'Before the debate' })).toBeTruthy();
  await user.press(screen.getAllByRole('button', { name: 'Debate' }).at(-1)!);

  // What went: the answers, the combined answer, its writer as the moderator, the tier.
  await waitFor(() => expect(agent.debateRequests()).toHaveLength(1));
  const body = agent.debateRequests()[0];
  expect(body).toMatchObject({
    question: 'When is the first ferry?',
    moderator: 'claude',
    tier: 'standard',
    judges: ['claude', 'codex', 'opencode'],
    answers: [
      { agent: 'claude', via: 'pc', text: answers.claude },
      { agent: 'codex', via: 'pc', text: answers.codex },
      { agent: 'opencode', via: 'pc', text: answers.opencode },
    ],
  });
  expect(body.synthesis).toMatch(/^Fake combined answer\./);
  expect(body.clientDebateId).toBeTruthy();
  expect((await loadAskPrefs()).debateNoticed).toBe(true);

  // The chat room as the PC reports it, step by step.
  expect(await screen.findByText('First ferry time', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByText('Fact')).toBeTruthy();
  expect(screen.getByText('Claude: 7 a.m.')).toBeTruthy();
  expect(screen.getByText('Codex, OpenCode: 8 a.m.')).toBeTruthy();
  expect(screen.getByText('Where to buy tickets')).toBeTruthy();
  expect(screen.getByText('Not debated')).toBeTruthy();
  expect(await screen.findByText('Moderator → Claude: Question to Claude?', {}, { timeout: 5000 })).toBeTruthy();
  expect(await after()).toBeTruthy();
  expect(screen.getByText('Claude turn: the timetable says 8 a.m.')).toBeTruthy();
  expect(screen.getByText('Keeps')).toBeTruthy();
  expect(screen.getByText('Changes')).toBeTruthy();
  expect(screen.getByText('Agreed')).toBeTruthy();
  expect(screen.getByText('The first ferry is at 8 a.m. (Claude changed).')).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Debate · 1 point' })).toBeTruthy();
  expect(screen.getByRole('header', { name: 'After the debate' })).toBeTruthy();
  expect(screen.getByText('debate 1 min 30 s')).toBeTruthy();
  // The combined answer stays; no Stop and no second debate once it is done.
  expect(screen.getByText('Fake combined answer.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Stop the debate' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Debate' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Debate again' })).toBeNull();

  // Copy takes the answer after the debate, AI names as words.
  const copies = screen.getAllByRole('button', { name: 'Copy' });
  await user.press(copies.at(-1)!);
  expect(await screen.findAllByText('Copied.')).toBeTruthy();
});

test('after the notice the next debate starts at once', async () => {
  const user = await open({ debateNoticed: true });
  await ask(user, 'When is the first ferry?');
  await written();
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(screen.queryByRole('header', { name: 'Before the debate' })).toBeNull();
  await waitFor(() => expect(agent.debateRequests()).toHaveLength(1));
  expect(await after()).toBeTruthy();
});

test('stop the debate: what was said stays, no answer after it', async () => {
  agent.debateReply = 'hold';
  const user = await open({ debateNoticed: true });
  await ask(user, 'When is the first ferry?');
  await written();
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(await screen.findByLabelText('Claude is answering… 3 s', {}, { timeout: 5000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Stop the debate' }));
  expect(await screen.findByText('The debate was stopped. No answer after it was written.')).toBeTruthy();
  expect(agent.calls.filter((c) => c.method === 'POST' && /^\/v1\/debates\/[^/]+\/stop$/.test(c.path))).toHaveLength(1);
  expect(screen.getByText('No reply')).toBeTruthy();
  expect(screen.queryByText(DEBATE_AFTER)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Stop the debate' })).toBeNull();
  // A stopped debate may be started again (11-07, the user's request).
  agent.debateReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Debate again' }));
  await waitFor(() => expect(agent.debateRequests()).toHaveLength(2));
  expect(await after()).toBeTruthy();
  expect(screen.queryByText('The debate was stopped. No answer after it was written.')).toBeNull();
});

// #36 (11-09): the offer says what a debate does; while the debate runs a
// live card above the follow-up bar says where it is; with less motion
// (the test setup's phone setting) the answering ring does not turn.
test('the offer line, the live card while the debate runs, and less motion', async () => {
  agent.debateReply = 'hold';
  const user = await open({ debateNoticed: true });
  await ask(user, 'When is the first ferry?');
  await written();
  expect(screen.getByText("The AIs read each other's answers and debate where they differ.")).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(await screen.findByLabelText('Claude is answering… 3 s', {}, { timeout: 5000 })).toBeTruthy();
  // The live card is kept above the follow-up bar, outside the thread, and goes to the newest turn when pressed.
  const dock = screen.getByTestId('debate-dock');
  const card = within(dock).getByRole('button', { name: "Claude's turn. Point 1 of 1" });
  expect(card.props.accessibilityHint).toBe('Goes to the newest turn');
  expect(within(dock).getByText('Point 1 of 1')).toBeTruthy();
  // The thread follows the debate's end mark (not the thread's end: a newer turn may sit under it).
  expect(screen.getByTestId('debate-end')).toBeTruthy();
  await user.press(card);
  expect(screen.queryByTestId('avatar-ring-busy')).toBeNull();
  // The offer line is gone while the debate runs, and so is the card once it stops.
  expect(screen.queryByText("The AIs read each other's answers and debate where they differ.")).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Stop the debate' }));
  expect(await screen.findByText('The debate was stopped. No answer after it was written.')).toBeTruthy();
  expect(screen.queryByLabelText("Claude's turn. Point 1 of 1")).toBeNull();
  expect(screen.queryByTestId('debate-dock')).toBeNull();
  expect(screen.queryByTestId('debate-end')).toBeNull();
});

test('no point worth a debate, a failed plan, and a PC busy with another debate', async () => {
  agent.debateReply = 'none';
  const user = await open({ debateNoticed: true });
  await ask(user, 'When is the first ferry?');
  await written();
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(await screen.findByText("The AIs don't differ on anything worth a debate", {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Debate again' })).toBeNull();

  agent.debateReply = 'fail';
  await user.type(screen.getByLabelText('Follow up'), 'And the last ferry?');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(2), { timeout: 5000 });
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(await screen.findByText("The moderator's reply was not in the debate's format, also when asked again.", {}, { timeout: 5000 })).toBeTruthy();
  // It may be debated again.
  agent.debateReply = 'ok';
  agent.debateError = { status: 429, code: 'too_many_debates' };
  await user.press(screen.getByRole('button', { name: 'Debate again' }));
  expect(await screen.findByText('Another debate is running on the PC. Try again when it is over.')).toBeTruthy();
  agent.debateError = null;
  await user.press(screen.getByRole('button', { name: 'Debate again' }));
  expect(await after()).toBeTruthy();
});

test('a conversation kept with its debate opens with it, and one still running is followed again', async () => {
  const conn = await pairedPC();
  const base = (id: string, question: string): Turn =>
    ({
      id,
      question,
      req: { mode: 'ask', prompt: question, tier: 'standard', agents: [{ agent: 'claude' }, { agent: 'codex' }] },
      agents: ['claude', 'codex'],
      run: {
        runId: `run-${id}`,
        mode: 'ask',
        tier: 'standard',
        status: 'done',
        startedAt: 'x',
        parts: [
          { agent: 'claude', status: 'done', answer: 'A', ms: 1000 },
          { agent: 'codex', status: 'done', answer: 'B', ms: 1000 },
        ],
      },
      compare: {
        start: 'auto',
        judges: ['claude', 'codex'],
        earlier: [],
        synth: { agent: 'claude' },
        id: `cmp-${id}`,
        snap: { comparisonId: `cmp-${id}`, status: 'done', source: 'run', engine: 'pc', synthesis: { status: 'done', agent: 'claude', text: `Combined ${id}.`, ms: 9000 } },
      },
    }) as unknown as Turn;
  const kept = {
    ...base('t1', 'Kept question'),
    debate: {
      id: 'dbt-old',
      snap: {
        debateId: 'dbt-old',
        status: 'done',
        points: [{ id: 'p1', topic: 'Kept point', nature: 'taste', debate: true, sides: [], turns: [], result: 'kept', line: 'Both names stay.' }],
        after: { text: 'Kept answer after the debate.', agent: 'claude', ms: 5000 },
        calls: 6,
        ms: 146000,
      },
    },
  } as unknown as Turn;
  await conversations.save({ id: 'conv-d', pcId: conn.id, startedAt: 1, title: 'Kept with a debate', turns: [kept] });
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'Kept with a debate' }));
  expect(await screen.findByText('Kept answer after the debate.')).toBeTruthy();
  expect(screen.getByText('Kept point')).toBeTruthy();
  expect(screen.getByText('Taste')).toBeTruthy();
  expect(screen.getByText('Options kept')).toBeTruthy();
  expect(screen.getByText('debate 2 min 26 s')).toBeTruthy();
  // A done debate is not asked for again.
  await later();
  expect(agent.calls.filter((c) => c.path.startsWith('/v1/debates'))).toHaveLength(0);
});

test('in Korean the debate reads in Korean', async () => {
  setLang('ko');
  const user = await open({ debateNoticed: true });
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  await written();
  expect(screen.getByText('AI를 약 10번 더 씁니다 · 2~3분')).toBeTruthy();
  expect(screen.getByText('AI들이 서로의 답을 보고, 답이 다른 곳을 토론합니다.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '토론하기' }));
  expect(await after()).toBeTruthy();
  for (const word of ['팩트', '유지', '바꿈', '합의', '토론하지 않음']) expect(screen.getByText(word)).toBeTruthy();
  expect(screen.getByRole('header', { name: '토론 · 쟁점 1개' })).toBeTruthy();
  expect(screen.getByRole('header', { name: '토론 뒤 정리' })).toBeTruthy();
  expect(screen.getByText('토론 1분 30초')).toBeTruthy();
});
