// P2-3 / C6: a question compared across the AIs on 한 번에, against a
// pretend agent whose AIs write combined answers and, like the agent since
// C6, judge no more (fakeAgent enableSynth): who is answering, follow-ups
// and the earlier questions they carry, lost requests and polls, a
// comparison the PC lost or stopped, too few answers, conversations kept
// before C6, and Korean. The combined answer itself is synth-test.tsx's.
// Every user event is awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { conversations } from '@/ask/conversations';
import type { Turn } from '@/ask/thread';
import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
const all = ['claude', 'codex', 'opencode'];

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
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

async function follow(user: User, question: string, field = 'Follow up', send = 'Send') {
  await user.type(screen.getByLabelText(field), question);
  await user.press(screen.getByRole('button', { name: send }));
}

// The combined answer of the latest turn is written.
const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const writing = () => screen.findByLabelText('Writing the combined answer…', {}, { timeout: 5000 });
const notFound = /^The PC no longer has this comparison/;
// renderRouter runs fake timers: time passes only when advanced.
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('while the AIs answer: who is at it, their models without the variant, and the follow-up waits', async () => {
  agent.runLimit = 1;
  const user = await open({ models: { opencode: 'opencode-go/deepseek-v4.1-flash#high' } });
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  await ask(user, 'Tell me about Seoul');

  expect(await screen.findByRole('header', { name: 'Answering…' })).toBeTruthy();
  expect(screen.getByText('Tell me about Seoul')).toBeTruthy();
  const row = (agentCode: string) => screen.getByTestId(`answering-${agentCode}`);
  await waitFor(() => expect(row('claude')).toHaveAccessibilityValue({ text: 'Answered, 1 s' }));
  expect(row('codex')).toHaveAccessibilityValue({ text: /^Answering, \d+ s$/ });
  expect(row('codex')).toBeBusy();
  expect(row('claude')).not.toBeBusy();
  // R-39: the model as people read it.
  expect(within(row('opencode')).getByText('deepseek-v4.1-flash')).toBeTruthy();
  expect(screen.queryByText(/#high|opencode-go\//)).toBeNull();
  expect(screen.getByText('Once every answer is in, the moderator AI writes a combined answer.')).toBeTruthy();
  // A follow-up written while they answer waits (TEST-1: with text in the box, so only the wait disables it).
  await user.type(screen.getByLabelText('Follow up'), 'next');
  const send = screen.getByRole('button', { name: 'Send' });
  expect(send).toBeDisabled();
  expect(send.props.accessibilityHint).toBe('You can send once every answer is in.');
  expect(agent.runRequests()[0].agents[2]).toEqual({ agent: 'opencode', model: 'opencode-go/deepseek-v4.1-flash#high' });

  agent.runLimit = Infinity;
  await written();
  expect(screen.queryByRole('header', { name: 'Answering…' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled(); // the follow-up kept, ready to go
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(screen.queryByText(/#high/)).toBeNull();
});

test('a follow-up to one AI is not compared: its answer in full', async () => {
  const user = await open();
  await ask(user, 'first');
  await written();
  agent.askReplies = {};
  await user.press(screen.getByRole('button', { name: /^Codex's answer\./ }));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  await waitFor(() => expect(screen.queryByTestId('answer-sheet')).toBeNull());
  await follow(user, 'only you', 'Ask Codex a follow-up', 'Send to Codex');
  expect(await screen.findByText('codex says: only you')).toBeTruthy();
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
  expect(agent.runRequests()[1].compare).toBeUndefined();
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);
  expect(screen.getAllByRole('header', { name: 'Combined answer' })).toHaveLength(1);
  expect(agent.comparisonCount()).toBe(1);
});

test('an older agent whose AIs do not write: each answer in full, the plain intro', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  expect(screen.queryByText(/moderator/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByText(answers.opencode)).toBeTruthy();
  expect(screen.getByLabelText("Claude's answer")).toBeTruthy();
  expect(screen.queryByRole('header', { name: 'Comparison' })).toBeNull();
  expect(screen.queryByRole('header', { name: 'Combined answer' })).toBeNull();
  expect(agent.runRequests()[0].compare).toBeUndefined();
});

test('a conversation left while the combined answer was written goes on, and comes back from the drawer as it is (#23)', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  await writing();
  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(await screen.findByText('The question before is still being answered and is kept in the history.')).toBeTruthy();

  // Back while it is still written: no second follow, nothing asked again.
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'q' }));
  expect(await writing()).toBeTruthy();
  agent.synthReply = 'ok'; // the writing already started finishes
  expect(await written()).toBeTruthy();
  expect(screen.queryByLabelText('Writing the combined answer…')).toBeNull();
  expect(agent.comparisonCount()).toBe(1);
  expect(agent.compareCalls('POST', '/cancel')).toHaveLength(0);
  expect(agent.runRequests()).toHaveLength(1);
  // Done: the polling stops.
  const gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);
});

test('#23: New question while the AIs answer: the question before goes on and is kept, and the next one waits for it', async () => {
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'slow one');
  await screen.findByText('Answering…');
  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(await screen.findByText('The question before is still being answered and is kept in the history.')).toBeTruthy();

  await ask(user, 'next one');
  expect(await screen.findByText('Waiting: it goes once the question before it is done.')).toBeTruthy();
  expect(agent.runRequests()).toHaveLength(1);
  expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();

  agent.runLimit = Infinity;
  // The one before ends with its combined answer, kept in the history; then this one goes.
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2), { timeout: 5000 });
  expect(agent.runRequests()[1].prompt).toBe('next one');
  expect(await written()).toBeTruthy();
  expect(screen.queryByText('Waiting: it goes once the question before it is done.')).toBeNull();
  expect(agent.compareCalls('POST', '/cancel')).toHaveLength(0);
  const before = conversations.list().find((c) => c.title === 'slow one');
  const kept = before && (await conversations.load(before.id));
  expect(kept?.turns[0].run?.status).toBe('done');
  expect(kept?.turns[0].compare?.snap?.synthesis?.status).toBe('done');
});

test('a comparison stopped on the PC (from another device) before it was made is started over by Compare again', async () => {
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'slow one');
  await screen.findByText('Answering…');
  await waitFor(() => expect(agent.comparisonIds()).toHaveLength(1));
  const [id] = agent.comparisonIds();
  await agent.fetch(`https://${HOST}:${PORT}/v1/comparisons/${id}/cancel`, { method: 'POST', headers: { Authorization: `Bearer ${TOKEN}` } });
  agent.runLimit = Infinity;

  expect(await screen.findByText('The comparison was stopped.', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByText('The combined answer was stopped.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await written()).toBeTruthy();
  expect(agent.compareCalls('POST', '/retry')).toHaveLength(1);
  expect(agent.comparisonCount()).toBe(1);
  expect(screen.queryByText('The comparison was stopped.')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();
});

test('a follow-up sends the earlier questions every writer answered, not one sent to one AI', async () => {
  agent.askReplies = {};
  const user = await open();
  await ask(user, 'first');
  await written();
  await user.press(screen.getByRole('button', { name: /^Codex's answer\./ }));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  await follow(user, 'only you', 'Ask Codex a follow-up', 'Send to Codex');
  await screen.findByText('codex says: only you');
  await follow(user, 'all again');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].compare).toEqual({ earlier: ['first'], judges: all, synthesis: { agent: 'claude' } });
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(2), { timeout: 5000 });
});

test('a comparison the PC lost (404) is made again from the answers, with the combined answer and the tier', async () => {
  agent.synthReply = 'hold';
  const user = await open({ tier: 'quick' });
  await ask(user, 'q');
  await writing();
  agent.forgetComparisons();
  expect(await screen.findByText(notFound)).toBeTruthy();

  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body).toEqual({
    clientComparisonId: expect.stringMatching(/^[0-9a-f-]{36}$/),
    question: 'q',
    earlier: [],
    judges: all,
    answers: all.map((a) => ({ id: a, agent: a, via: 'pc', model: `${a}-quick`, text: answers[a as keyof typeof answers] })),
    synthesis: { agent: 'claude', tier: 'quick' },
  });
  const [first, second] = agent.comparisonIds();
  expect(second).not.toBe(first);
  expect(agent.compareCalls('GET', `/${second}`).length).toBeGreaterThan(0);
  expect(agent.comparisonCount('direct')).toBe(1);
  expect(screen.queryByText(notFound)).toBeNull();
});

test('a run the PC no longer has (404 run_not_found) keeps what came; its comparison is made again from those answers (R-80)', async () => {
  agent.runLimit = 2; // OpenCode is still answering when the phone stops asking
  const user = await open();
  await ask(user, 'q');
  await waitFor(() => expect(agent.calls.filter((c) => c.method === 'GET' && c.path.startsWith('/v1/runs/')).length).toBeGreaterThan(0));
  await later();
  await later();
  // The phone slept past the PC's keeping (a day): the run and its comparison are gone.
  agent.forgetRuns();
  agent.forgetComparisons();
  expect(await screen.findByText(notFound, {}, { timeout: 5000 })).toBeTruthy();
  // No dead Try again, no error code; what came stays.
  expect(screen.queryByText(/run_not_found/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body.answers.map((a: { agent: string }) => a.agent)).toEqual(['claude', 'codex']);
  // OpenCode's answer says why it is missing.
  expect(screen.getAllByText(/^The PC no longer has this answer/).length).toBeGreaterThan(0);
});

test('a refused Compare again says why and offers Compare with a new request', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  await writing();
  agent.forgetComparisons();
  await screen.findByText(notFound);
  agent.comparisonError = { status: 429, code: 'too_many_comparisons' };
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await screen.findByText('The PC is still comparing other answers. Tap Compare in a moment.')).toBeTruthy();

  agent.comparisonError = null;
  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Compare' }));
  expect(await written()).toBeTruthy();
  const keys = agent.callsTo('POST', '/v1/comparisons').map((c) => c.body.clientComparisonId);
  expect(keys).toHaveLength(2);
  expect(keys[1]).not.toBe(keys[0]); // the PC answered the first, so its key is done
  expect(agent.comparisonCount('direct')).toBe(1);
});

test('a lost POST /v1/runs: Try again sends the same run, and it is compared once', async () => {
  agent.loseReplies['POST /v1/runs'] = 3;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText(/^Can't reach the PC/, {}, { timeout: 6000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await written()).toBeTruthy();
  const ids = agent.runRequests().map((r) => r.clientRunId);
  expect(ids).toHaveLength(4);
  expect(new Set(ids).size).toBe(1);
  expect(agent.comparisonCount()).toBe(1);
  expect(agent.synthCalls).toBe(1);
});

test('a lost POST /v1/comparisons: Try again sends the same clientComparisonId, one comparison', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  await writing();
  agent.forgetComparisons();
  await screen.findByText(notFound);
  agent.synthReply = 'ok';
  agent.loseReplies['POST /v1/comparisons'] = 3;
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await screen.findByText(/^Can't reach the PC/, {}, { timeout: 6000 })).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await written()).toBeTruthy();
  const keys = agent.callsTo('POST', '/v1/comparisons').map((c) => c.body.clientComparisonId);
  expect(keys).toHaveLength(4);
  expect(new Set(keys).size).toBe(1);
  expect(agent.comparisonCount('direct')).toBe(1);
});

test('the PC stops answering polls: Try again follows the same comparison, no new one', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  await writing();
  agent.reach = 'offline';
  // 30 dropped looks in a row, a second apart, end the follow.
  await act(() => jest.advanceTimersByTimeAsync(35000));
  expect(await screen.findByText(/^Can't reach the PC/)).toBeTruthy();
  agent.reach = 'ok';
  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await written()).toBeTruthy();
  expect(agent.callsTo('POST', '/v1/comparisons')).toHaveLength(0);
  expect(agent.comparisonCount()).toBe(1);
});

test('after a 10 KiB question the follow-up sends it cut to 4 KiB on a character boundary', async () => {
  agent.askReplies = {};
  const user = await open();
  const big = '가'.repeat(1365) + '😀' + '가'.repeat(2000);
  await user.paste(await screen.findByLabelText('Ask a question'), big);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await written();
  await follow(user, 'next');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1].compare.earlier).toEqual(['가'.repeat(1365)]); // 4,095 bytes; the emoji would make 4,099
  expect(agent.compareErrors).toEqual([]);
  expect(agent.comparisonCount()).toBe(2);
});

test('two AIs that failed to answer leave nothing to compare; with none, it does not say one did', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.askReplies.opencode = { code: 'ask_timeout' };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Only one AI answered, so there is nothing to compare.', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByRole('button', { name: /^Codex's answer\. codex-standard\. The AI reported an error\./ })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'AIs that answered: 1 (Claude). Details' })).toBeTruthy();
  expect(agent.synthCalls).toBe(0);

  await user.press(screen.getByRole('button', { name: 'New question' }));
  agent.askReplies.claude = { code: 'ask_failed', message: 'x' };
  await ask(user, 'q2');
  expect(await screen.findByText('Comparing needs at least two answers.', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByText('Only one AI answered, so there is nothing to compare.')).toBeNull();
});

// C6d (#24): conversations kept on the phone before C6 hold what judging
// left: snapshots with requests, judgments, cards, counts and topics, turns
// of two AIs that were never compared ('noJudgeOff'), and turns that waited
// for Compare ('manual'). They open as the app shows things now.
test('a conversation kept before C6 opens: its combined answer without the judging, two AIs in full, Compare by hand', async () => {
  const conn = await pairedPC();
  const part = (agent: string) => ({ agent, status: 'done', answer: answers[agent as keyof typeof answers], model: `${agent}-standard`, ms: 1234, session: `${agent}:s-old` });
  const base = (id: string, question: string, agents: string[]) => ({
    id,
    question,
    req: { mode: 'ask', prompt: question, tier: 'standard', agents: agents.map((a) => ({ agent: a })), clientRunId: id },
    agents,
    at: 1,
    run: { runId: `run-${id}`, mode: 'ask', tier: 'standard', status: 'done', startedAt: 'x', parts: agents.map(part) },
  });
  const judged = {
    ...base('old-1', 'Seoul before C6', all),
    compare: {
      start: 'auto',
      judges: all,
      earlier: [],
      synth: { agent: 'claude' },
      id: 'cmp-old',
      snap: {
        comparisonId: 'cmp-old',
        status: 'done',
        source: 'run',
        engine: 'pc',
        answers: all.map((a) => ({ id: a, agent: a, via: 'pc', family: a, status: 'ready', notJudged: 0, units: [{ id: `${a}:1`, n: 1, kind: 'sentence', text: 'Seoul is the capital.', start: 0, end: 21, judged: true }] })),
        requests: [{ id: 'r1', from: 'claude', against: 'codex', judge: 'opencode', judgeOnly: false, status: 'failed', attempt: 1, error: { code: 'judge_failed' } }],
        judgments: [{ unit: 'claude:1', against: 'codex', request: 'r1', judge: 'opencode', core: true, label: 'opposite', evidence: ['codex:1'], unsure: false, flags: [] }],
        items: [{ key: 'd:claude:1', kind: 'differ', positions: [{ agents: ['claude'], units: ['claude:1'] }, { agents: ['codex'], units: ['codex:1'] }], check: [], missing: [] }],
        counts: { differ: 1, check: 0, agree: 0, onlyOne: 0, notCompared: 0, notJudged: 0 },
        synthesis: {
          status: 'done',
          agent: 'claude',
          model: 'sonnet',
          ms: 20400,
          text: 'Combined before C6.',
          judged: true,
          unnamedSides: 1,
          topics: { status: 'done', items: [{ key: 'd:claude:1', topic: 'Population', gists: ['9.4', '9.7'] }] },
        },
      },
    },
  };
  const two = { ...base('old-2', 'Two before', ['claude', 'codex']), compare: { start: 'noJudgeOff', judges: [], earlier: [] } };
  const manual = { ...base('old-3', 'By hand before', all), compare: { start: 'manual', judges: all, earlier: [], synth: { agent: 'claude' } } };
  await conversations.save({ id: 'conv-old', pcId: conn.id, startedAt: 1, title: 'Seoul before C6', turns: [judged, two, manual] as unknown as Turn[] });

  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'Seoul before C6' }));

  // The combined answer and its times; nothing of the judging.
  expect(await screen.findByText('Combined before C6.')).toBeTruthy();
  expect(screen.getByText('answers 1 s · combined in 20 s')).toBeTruthy();
  for (const gone of [/^Where the AI answers differ/, /^Evidence$/, /^Some places where the AIs differ/, /^Written from the answers alone/, /^Only two AIs were asked/, /judg/i]) {
    expect(screen.queryByText(gone)).toBeNull();
  }
  expect(screen.queryByRole('button', { name: /^Differ: / })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Judging details' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();
  // The two AIs of the second turn: each answer in full.
  expect(screen.getByLabelText("Claude's answer")).toBeTruthy();
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
  // Nothing was asked of the PC for them.
  expect(agent.compareCalls('GET')).toHaveLength(0);

  // The third waited for Compare: it is made from the answers, with the combined answer.
  await user.press(screen.getByRole('button', { name: 'Compare' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body).toMatchObject({ question: 'By hand before', judges: all, synthesis: { agent: 'claude', tier: 'standard' } });
  expect(agent.comparisonCount('direct')).toBe(1);
});

test('in Korean the comparison reads in Korean', async () => {
  setLang('ko');
  agent.synthReply = 'hold';
  const user = await open();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  expect(await screen.findByLabelText('종합 답변 쓰는 중…', {}, { timeout: 5000 })).toBeTruthy();
  agent.forgetComparisons();
  expect(await screen.findByText(/^PC에 이 비교가 없습니다/)).toBeTruthy();
  expect(screen.getByRole('header', { name: '비교' })).toBeTruthy();
  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: '다시 비교' }));
  await written();
  expect(screen.getByRole('header', { name: '종합 답변' })).toBeTruthy();
  expect(screen.getByRole('header', { name: 'AI별 답 3개' })).toBeTruthy();
  expect(screen.getByText(/^답 1초 · 종합 15초$/)).toBeTruthy();
});
