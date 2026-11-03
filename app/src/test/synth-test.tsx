// P2-3 S5b / S5c: the combined answer on 한 번에, against a pretend agent
// whose AIs judge each other's answers and then one writes them into one
// (fakeAgent enableSynth, fakeCompare combinedText): its two steps (R-38),
// the answer with where the AIs differ, the judging details, the evidence
// folded under it, writing it by hand or again, and Korean. Every user
// event is awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { StyleSheet } from 'react-native';

import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { aiPalettes } from '@/ui/theme';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

// Claude says 9.4 million, Codex and OpenCode 9.7 million: one Differ item,
// one Agree item (the capital), and two Only one items.
const answers = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
const differs = 'The AIs differ. Claude: It has 9.4 million people. Codex, OpenCode: It has 9.7 million people.';
// On screen the AIs the writer names in braces are chips set off by thin spaces (#6).
const chip = (name: string) => ` ${name} `;
const differsShown = `The AIs differ. ${chip('Claude')}: It has 9.4 million people. ${chip('Codex')}, ${chip('OpenCode')}: It has 9.7 million people.`;
const tilesDone = ['Differ: 1. Show', 'Check this: 0. Show', 'Agree: 1. Show', 'Only one: 2. Show'];
const company = "The writing AI's company receives the question and every answer (with the AIs' names).";
const mayErr = 'An AI wrote this combined answer, so it can be wrong.';
const nextStep = 'Next step: Writing the combined answer…';

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

const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const tile = (name: string) => screen.queryByRole('button', { name });
const skeleton = () => screen.queryByTestId('skeleton', { includeHiddenElements: true });
const close = () => screen.getByRole('button', { name: 'Close' });
// renderRouter runs fake timers: time passes only when advanced.
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('while the AIs compare, writing the combined answer waits as the next step (R-38); then it is written', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'Tell me about Seoul');
  expect(await screen.findByText('Once every answer is in, they are compared and a combined answer is written.')).toBeTruthy();

  // Judging: step one is current; writing is only the next step (the phone check said 'Combining…' here).
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByLabelText(nextStep)).toBeTruthy();
  expect(screen.queryByLabelText('Writing the combined answer…')).toBeNull();
  expect(screen.queryByText('Combining…')).toBeNull();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(skeleton()).toBeTruthy();
  // The evidence is folded under it; only Differ can be counted yet.
  for (const name of ['Differ: 1. Show', 'Check this: still comparing', 'Agree: still comparing', 'Only one: still comparing']) expect(tile(name)).toBeTruthy();
  expect(screen.queryByTestId('evidence-differ')).toBeNull();

  // The judging ended, the writing goes on: step one done, step two current.
  agent.synthReply = 'hold';
  agent.compareHold = false;
  expect(await screen.findByLabelText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByLabelText('Judgments: 6')).toBeTruthy();
  expect(screen.queryByLabelText(nextStep)).toBeNull();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
  expect(skeleton()).toBeTruthy();
  for (const name of tilesDone) expect(tile(name)).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();

  agent.synthReply = 'ok';
  expect(await written()).toBeTruthy();
  expect(screen.getByText(differsShown)).toBeTruthy();
  // #6: a named AI is a chip in its own avatar colour; no braces show.
  const chipLook = (name: string) => StyleSheet.flatten(screen.getAllByText(chip(name))[0].props.style);
  expect(chipLook('Claude')).toMatchObject({ fontWeight: '600', backgroundColor: aiPalettes.light.sky.bg, color: aiPalettes.light.sky.fg });
  expect(chipLook('Codex')).toMatchObject({ backgroundColor: aiPalettes.light.lilac.bg, color: aiPalettes.light.lilac.fg });
  expect(chipLook('OpenCode')).toMatchObject({ backgroundColor: aiPalettes.light.teal.bg, color: aiPalettes.light.teal.fg });
  expect(screen.queryByText(/\{\{/)).toBeNull();
  expect(screen.queryByLabelText(/Writing the combined answer/)).toBeNull();
  expect(skeleton()).toBeNull();
  expect(screen.getByText(mayErr)).toBeTruthy();
  expect(screen.getByText('answers 1 s · compared in 10 s')).toBeTruthy();
  expect(screen.queryByText(/^Written from the answers alone/)).toBeNull();
  expect(screen.queryByText(/^Some places where the AIs differ/)).toBeNull();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'], synthesis: { agent: 'claude' } });
  expect(agent.synthCalls).toBe(1);

  // Done: the polling stops.
  const gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);

  // The evidence opens from a count and folds again.
  await user.press(screen.getByRole('button', { name: 'Only one: 2. Show' }));
  expect(screen.getByTestId('evidence-differ')).toBeTruthy();
  expect(within(screen.getByTestId('evidence-check')).getByText('No sentences here.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Hide' }));
  expect(screen.queryByTestId('evidence-differ')).toBeNull();
  expect(tile('Differ: 1. Show')).toBeTruthy();
});

test('where the AIs differ is a highlight in the answer that opens their own sentences, each leading to its answer', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  expect(screen.getByText(/^The AIs differ: /)).toBeTruthy();
  // The whole line is the button, with its state (UX-1).
  const headline = screen.getByRole('button', { name: 'The AIs differ: Claude 9.4, Codex·OpenCode 9.7' });
  expect(headline).toBeCollapsed();
  expect(screen.queryByTestId('differ-panel')).toBeNull();

  await user.press(headline);
  const panel = within(screen.getByTestId('differ-panel'));
  expect(screen.getByRole('button', { name: 'The AIs differ: Claude 9.4, Codex·OpenCode 9.7' })).toBeExpanded();
  expect(panel.getByRole('button', { name: 'Claude: It has 9.4 million people.' })).toBeTruthy();
  expect(panel.getByRole('button', { name: 'Codex: It has 9.7 million people.' })).toBeTruthy();
  expect(panel.getByRole('button', { name: 'OpenCode: It has 9.7 million people.' })).toBeTruthy();
  expect(panel.getByText('9.4')).toHaveStyle({ fontWeight: '700' });

  await user.press(panel.getByRole('button', { name: 'OpenCode: It has 9.7 million people.' }));
  const sheet = within(await screen.findByTestId('answer-sheet'));
  expect(sheet.getByRole('header', { name: 'OpenCode' })).toBeTruthy();
  expect(sheet.getByText(answers.opencode)).toBeTruthy();
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('answer-sheet')).toBeNull());

  await user.press(screen.getByRole('button', { name: 'The AIs differ: Claude 9.4, Codex·OpenCode 9.7' }));
  expect(screen.queryByTestId('differ-panel')).toBeNull();
});

test('a differ line that can only count the AIs says so alone; a whole answer on the other side is named (UX-6, COR-1)', async () => {
  // No numbers marked on either side: the count of AIs, with no 'The AIs differ:' in front.
  agent.itemsTweak = (items) => items.map((it) => ({ ...it, positions: it.positions.map((p) => ({ agents: p.agents, units: p.units })) }));
  const user = await open();
  await ask(user, 'q');
  await written();
  expect(screen.getByRole('button', { name: "The 3 AIs' answers differ" })).toBeCollapsed();
  expect(screen.queryByText(/^The AIs differ: /)).toBeNull();

  // Codex's whole answer against Claude's sentence (no evidence): both AIs counted and named.
  agent.itemsTweak = (items) =>
    items.map((it) => (it.kind === 'differ' ? { ...it, positions: [it.positions[0], { agents: ['codex'], units: [] }] } : it));
  await user.press(screen.getByRole('button', { name: 'New question' }));
  await ask(user, 'q2');
  await written();
  const line = screen.getByRole('button', { name: "The 2 AIs' answers differ" });
  await user.press(line);
  expect(within(screen.getByTestId('differ-panel')).getByRole('button', { name: 'Claude: It has 9.4 million people.' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Differ: 1. Show' }));
  expect(within(screen.getByTestId('evidence-differ')).getByText('Claude ↔ Codex')).toBeTruthy();
});

test('Judging details: who wrote it, every request under its judge, and the notes', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(screen.getByRole('button', { name: 'Judging details' }));
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByRole('header', { name: 'Judging details' })).toBeTruthy();
  expect(judges.getByText('answers 1 s · compared in 10 s')).toBeTruthy();
  expect(judges.getByRole('header', { name: 'Combined answer written by' })).toBeTruthy();
  expect(judges.getByLabelText('Claude, sonnet, 15.2 s')).toBeTruthy();
  expect(judges.getByText('Judgments: 6')).toBeTruthy();
  expect(judges.getByText('sentences → answer', { includeHiddenElements: true })).toBeTruthy();
  // The model goes in the same text as the name (one Android layout, S5c check).
  for (const name of ['Judged by Claude', 'Judged by Codex', 'Judged by OpenCode']) expect(judges.getByRole('header', { name: new RegExp(`^${name}, `) })).toBeTruthy();
  expect(judges.getByText('Codex → OpenCode')).toBeTruthy();
  expect(judges.getByLabelText("Claude's sentences against Codex's answer, 9.8 s")).toBeTruthy();
  expect(judges.getAllByText('9.8 s')).toHaveLength(6);
  for (const note of [
    mayErr,
    "The judging AI's company receives the question and the other AIs' answers.",
    company,
    'The judging accuracy has not been measured yet.',
    "The comparison is an AI's judgment too, and it can be wrong.",
  ])
    expect(judges.getByText(note)).toBeTruthy();
  // The notes are no longer under the answer.
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('judges-sheet')).toBeNull());
  expect(screen.queryByText(company)).toBeNull();
});

test('once the judging is measured, the details do not say it is not', async () => {
  agent.enableJudge({ verified: true, typicalMs: 40444 });
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(screen.getByRole('button', { name: 'Judging details' }));
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.queryByText('The judging accuracy has not been measured yet.')).toBeNull();
  expect(judges.getByText(company)).toBeTruthy();
});

test('Copy copies the combined answer’s Markdown', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(screen.getByRole('button', { name: 'Copy' }));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(`Fake combined answer.\n\n${differs}`);
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('the writing AI picked in Settings is asked for and writes', async () => {
  const user = await open({ synthAgent: 'codex' });
  await ask(user, 'q');
  await written();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ agent: 'codex' });
  await user.press(screen.getByRole('button', { name: 'Judging details' }));
  expect(within(await screen.findByTestId('judges-sheet')).getByLabelText('Codex, codex-std, 15.2 s')).toBeTruthy();
});

test('with the combined answer off: no request for one, the evidence open, and Write one sends it', async () => {
  const user = await open({ synth: false, synthAgent: 'opencode' });
  // The intro does not promise one.
  expect(await screen.findByText(/^One question goes to every AI turned on/)).toBeTruthy();
  expect(screen.queryByText(/^Once the answers are compared, one AI writes/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByText('Once every answer is in, they are compared.')).toBeTruthy();
  expect(await screen.findByRole('button', { name: 'Write a combined answer' }, { timeout: 5000 })).toBeTruthy();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ off: true });
  expect(agent.synthCalls).toBe(0);
  expect(screen.getByText("This uses one of the PC's AIs 1 more time.")).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Comparison' })).toBeTruthy();
  // Off: the evidence shows open, as in S5.
  expect(screen.getByTestId('evidence-differ')).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Write a combined answer' }));
  expect(await written()).toBeTruthy();
  expect(agent.synthRequests()).toEqual([{ agent: 'opencode' }]);
  expect(agent.synthCalls).toBe(1);
  expect(screen.queryByRole('button', { name: 'Write a combined answer' })).toBeNull();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  // Written now: the evidence folds under it.
  expect(tile('Differ: 1. Show')).toBeTruthy();
});

test('a writer that fails says why in its words; Write again writes it', async () => {
  agent.synthReply = 'fail';
  const user = await open();
  await ask(user, 'q');
  const why = 'The writing AI reported an error.\nusage limit reached';
  expect(await screen.findByText(why, {}, { timeout: 5000 })).toBeTruthy();
  // Failed: the evidence shows open.
  expect(screen.getByTestId('evidence-differ')).toBeTruthy();
  // The details say it too.
  await user.press(screen.getAllByRole('button', { name: 'Judging details' })[0]);
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByRole('header', { name: 'Combined answer written by' })).toBeTruthy();
  expect(judges.getByText(why)).toBeTruthy();
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('judges-sheet')).toBeNull());

  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Write again' }));
  expect(await written()).toBeTruthy();
  expect(agent.synthRequests()).toEqual([{ agent: 'claude' }]);
  expect(agent.synthCalls).toBe(2);
  expect(screen.queryByText(/^The writing AI reported an error/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Write again' })).toBeNull();
});

test('written without comparison results, or with names left out, it says so', async () => {
  agent.synthJudged = false;
  const user = await open();
  await ask(user, 'q');
  await written();
  expect(screen.getByText('Written from the answers alone, without comparison results.')).toBeTruthy();
  expect(screen.queryByText(/^Some places where the AIs differ/)).toBeNull();

  agent.synthJudged = undefined;
  agent.synthReply = 'unnamed';
  await user.type(screen.getByLabelText('Follow up'), 'again');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(2), { timeout: 5000 });
  expect(screen.getByText('Some places where the AIs differ may be missing from the combined answer. Check the evidence.')).toBeTruthy();
  expect(screen.getAllByText('Written from the answers alone, without comparison results.')).toHaveLength(1);
});

test('when every judge fails, the combined answer is still written from the answers alone', async () => {
  for (const a of ['claude', 'codex', 'opencode']) agent.judgeReplies[a] = { code: 'judge_failed', message: 'usage limit reached' };
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  // Writing after a comparison that failed: step one failed, step two current.
  expect(await screen.findByLabelText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByLabelText('Comparison')).toBeTruthy();
  agent.synthReply = 'ok';
  await written();
  expect(screen.getByText('The judging AI reported an error.\nusage limit reached')).toBeTruthy();
  expect(screen.getByText('Written from the answers alone, without comparison results.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Differ: / })).toBeNull(); // no evidence to fold
  // Once it is written, the judging may be tried again.
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await waitFor(() => expect(agent.compareCalls('POST', '/retry')).toHaveLength(1));
});

test('an agent that does not write combined answers: the comparison only, nothing asked', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableJudge();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  expect(await screen.findByText(/^One question goes to every AI turned on, at once\. Once the answers are in/)).toBeTruthy();
  expect(screen.queryByText(/^Once the answers are compared, one AI writes/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByText('Once every answer is in, they are compared.')).toBeTruthy();
  await waitFor(() => expect(screen.getByTestId('evidence-onlyOne')).toBeTruthy(), { timeout: 5000 });
  expect(screen.getByRole('header', { name: 'Comparison' })).toBeTruthy();
  expect(screen.queryByRole('header', { name: 'Combined answer' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Write a combined answer' })).toBeNull();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'] });
  expect(agent.synthRequests()).toEqual([]);
});

test('New question while the combined answer is written asks the PC to stop, once', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByLabelText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
  const [id] = agent.comparisonIds();
  let gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET').length).toBeGreaterThan(gets); // still polling

  await user.press(screen.getByRole('button', { name: 'New question' }));
  await waitFor(() => expect(agent.compareCalls('POST', '/cancel')).toHaveLength(1));
  expect(agent.compareCalls('POST', '/cancel')[0].path).toBe(`/v1/comparisons/${id}/cancel`);
  gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);
  expect(agent.compareCalls('POST', '/cancel')).toHaveLength(1);
  expect(screen.queryByLabelText('Writing the combined answer…')).toBeNull();
});

test('a comparison the PC lost is made again with the combined answer and the tier', async () => {
  agent.compareHold = true;
  const user = await open({ tier: 'quick' });
  await ask(user, 'q');
  await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 });
  agent.forgetComparisons();
  expect(await screen.findByText(/^The PC no longer has this comparison/)).toBeTruthy();

  agent.compareHold = false;
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body.synthesis).toEqual({ agent: 'claude', tier: 'quick' });
  expect(agent.synthCalls).toBe(1);
});

test('Compare by hand costs one more for the combined answer and sends it', async () => {
  const user = await open({ compare: false });
  await ask(user, 'q');
  expect(await screen.findByText("This uses the PC's AIs 7 more times.")).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
  // No comparison is promised before it is asked for.
  expect(screen.queryByText(/^Once every answer is in/)).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Compare' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body.synthesis).toEqual({ agent: 'claude', tier: 'standard' });
  expect(agent.comparisonCount('direct')).toBe(1);
  expect(screen.getByText(differsShown)).toBeTruthy();
});

test('in Korean the combined answer reads in Korean', async () => {
  setLang('ko');
  agent.compareHold = true;
  const user = await open();
  expect(await screen.findByText(t('ask.introSynth'))).toBeTruthy();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  expect(await screen.findByText('비교하는 중… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByLabelText('다음 단계: 종합 답변 쓰는 중…')).toBeTruthy();
  expect(screen.getByRole('button', { name: '확인 필요, 비교하는 중' })).toBeTruthy();
  agent.compareHold = false;
  await written();
  expect(screen.getByRole('header', { name: '종합 답변' })).toBeTruthy();
  expect(screen.getByText('AI가 쓴 종합이라 틀릴 수 있습니다.')).toBeTruthy();
  expect(screen.getByText(/^AI마다 다릅니다: /)).toBeTruthy();
  expect(screen.getByText('답 1초 · 비교 10초')).toBeTruthy();
  for (const name of ['다른 말 1개, 펼치기', '확인 필요 0개, 펼치기', '같은 말 1개, 펼치기', '한 AI만 2개, 펼치기']) expect(tile(name)).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '판정 정보' }));
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByRole('header', { name: '종합 답변을 쓴 AI' })).toBeTruthy();
  expect(judges.getByText('종합 답변을 쓰는 AI의 회사가 질문과 모든 답(AI 이름 포함)을 받습니다.')).toBeTruthy();
  expect(judges.getByText('판정 정확도를 아직 재지 않았습니다.')).toBeTruthy();
});
