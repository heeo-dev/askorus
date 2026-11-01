// P2-3: the 정리 (Summary) tab of 한 번에 묻기 against a pretend agent whose
// AIs judge each other's answers (fakeAgent enableJudge, fakeCompare).
// Every user event is awaited (R-07).

import { act, userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import * as ReactNative from 'react-native';
import { StyleSheet } from 'react-native';

import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';

// Claude says 9.4 million, Codex and OpenCode 9.7 million: one Differ item,
// one Agree item (the capital), and two Only one items.
const answers = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
const lost = 'The judging AI reported an error.\nusage limit reached';

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableJudge();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function pairedPC() {
  return new ConnectionStore().add(
    { kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' },
    TOKEN,
  );
}

async function ask(user: ReturnType<typeof userEvent.setup>, question: string, button = 'Ask') {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: button }));
}

async function open() {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  return user;
}

// The comparison is final once the chips other than Differ show a number.
const finished = (only = 'Only one') => screen.findByRole('button', { name: new RegExp(`^${only} \\d+$`) }, { timeout: 5000 });

const kinds = ['Differ', 'Check this', 'Agree', 'Only one'];
const cardHeads = (names = kinds) =>
  screen
    .queryAllByRole('header')
    .map((h) => String(h.props.children))
    .filter((x) => names.includes(x));

test('while the AIs answer, Summary says who is still at it and the tabs show it', async () => {
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'Tell me about Seoul');
  expect(await screen.findByText('AIs answered: 1 · My PC 1')).toBeTruthy();
  expect(screen.queryByText(/^AIs answered: 0/)).toBeNull();
  expect(screen.queryByText(/^Claude · answering…/)).toBeNull();
  expect(screen.getByText(/^Codex · answering… \d+ s$/)).toBeTruthy();
  expect(screen.getByText(/^OpenCode · answering… \d+ s$/)).toBeTruthy();
  expect(screen.getByText('Codex …')).toBeTruthy();
  expect(screen.queryByText('Claude …')).toBeNull(); // Claude's tab: answered

  agent.runLimit = Infinity;
  await finished();
  expect(screen.queryByText(/answering…/)).toBeNull();
  expect(screen.queryByText('Codex …')).toBeNull();
});

test('Summary is the default tab: Differ first, chips filter, the footer says who judged', async () => {
  const user = await open();
  expect(await screen.findByText(/the Summary tab shows where they differ and agree/)).toBeTruthy();
  // The companies get the data while judging, before the Summary shows anything (R-34).
  expect(
    screen.getByText(
      "One question goes to every AI turned on, at once. Answers show as they come. Once they are in, the AIs judge each other's answers and the Summary tab shows where they differ and agree. While judging, the judging AI's company receives the question and the other AIs' answers. Long-press an AI to pick its model.",
    ),
  ).toBeTruthy();
  await ask(user, 'Tell me about Seoul');
  await finished();

  expect(screen.getByRole('tab', { name: 'Summary' })).toBeSelected();
  expect(screen.getByRole('tab', { name: 'Codex' })).not.toBeSelected();
  expect(screen.getByText('AIs answered: 3 · My PC 3')).toBeTruthy();
  for (const name of ['Differ 1', 'Check this 0', 'Agree 1', 'Only one 2']) expect(screen.getByRole('button', { name })).toBeTruthy();
  expect(cardHeads()).toEqual(['Differ', 'Agree', 'Only one']); // the empty Check this card is left out
  expect(screen.getByText('Where the AIs said different things. Check them yourself.')).toBeTruthy();

  // Differ: Claude's sentence, who said what, the numbers line, the digits bold.
  expect(screen.getByText('It has 9.4 million people.')).toBeTruthy();
  expect(screen.getByText('Claude ↔ Codex, OpenCode')).toBeTruthy();
  expect(screen.getByText('The numbers differ · check them yourself')).toBeTruthy();
  expect(screen.getByText('9.4')).toHaveStyle({ fontWeight: '700' });
  // Agree and Only one.
  expect(screen.getByText('Seoul is the capital.')).toBeTruthy();
  expect(screen.getByText('Claude, Codex, OpenCode')).toBeTruthy();
  expect(screen.getByText('Try the palace.')).toBeTruthy();
  expect(screen.getByText('Only Claude')).toBeTruthy();
  expect(screen.getByText('Only OpenCode')).toBeTruthy();

  // A chip shows its card alone; pressed again, all of them.
  await user.press(screen.getByRole('button', { name: 'Agree 1' }));
  expect(screen.getByRole('button', { name: 'Agree 1' })).toBeSelected();
  expect(cardHeads()).toEqual(['Agree']);
  expect(screen.queryByText('Claude ↔ Codex, OpenCode')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Agree 1' }));
  expect(cardHeads()).toEqual(['Differ', 'Agree', 'Only one']);

  expect(screen.getByText('Judged by: Claude haiku · Codex gpt-6-luna · OpenCode deepseek-v4.1-flash ›')).toBeTruthy();
  expect(screen.getByText("The judging AI's company receives the question and the other AIs' answers.")).toBeTruthy();
  expect(screen.getByText("The comparison is an AI's judgment too, and it can be wrong.")).toBeTruthy();
  expect(screen.getByText('The judging accuracy has not been measured yet.')).toBeTruthy();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'] });

  // An AI's tab: who judged its answer, then its answer card as in P2-2.
  await user.press(screen.getByRole('tab', { name: 'Codex' }));
  expect(screen.getByRole('tab', { name: 'Codex' })).toBeSelected();
  expect(screen.getByText('My PC · Judged by: Claude, OpenCode')).toBeTruthy();
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
  expect(screen.getByText(answers.codex)).toBeTruthy();
  expect(screen.getByText('Only this AI')).toBeTruthy();
  expect(screen.queryByText('Claude ↔ Codex, OpenCode')).toBeNull();
});

test('See both sentences shows each side and every direction; an answer link opens that tab', async () => {
  const user = await open();
  await ask(user, 'q');
  await finished();
  await user.press(screen.getByRole('button', { name: 'See both sentences ›' }));
  expect(await screen.findByText('Judgments by direction')).toBeTruthy();

  expect(screen.getAllByText('It has 9.4 million people.')).toHaveLength(2); // the card and the sheet
  expect(screen.getAllByText('It has 9.7 million people.')).toHaveLength(2); // Codex's and OpenCode's
  expect(screen.getByText('Codex, OpenCode')).toBeTruthy();
  for (const d of screen.getAllByText('9.7')) expect(d).toHaveStyle({ fontWeight: '700' });
  for (const line of [
    "OpenCode: Claude's sentence vs Codex's answer · Differs",
    "OpenCode: Codex's sentence vs Claude's answer · Differs",
    "Codex: Claude's sentence vs OpenCode's answer · Differs",
    "Codex: OpenCode's sentence vs Claude's answer · Differs",
    "Claude: Codex's sentence vs OpenCode's answer · Same",
    "Claude: OpenCode's sentence vs Codex's answer · Same",
  ])
    expect(screen.getByText(line)).toBeTruthy();
  expect(screen.queryByText(/%/)).toBeNull(); // no probabilities

  await user.press(screen.getByRole('button', { name: "See Codex's answer" }));
  await waitFor(() => expect(screen.queryByText('Judgments by direction')).toBeNull());
  expect(screen.getByRole('tab', { name: 'Codex' })).toBeSelected();
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
});

test('a judge that is not sure puts the sentence under Check this; tapping it says why', async () => {
  agent.judgeReplies.opencode = 'unsure';
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(screen.getByRole('button', { name: 'Check this 3' })).toBeTruthy();
  expect(cardHeads()).toEqual(['Differ', 'Check this', 'Only one']);
  expect(screen.getAllByText("Claude's sentence")).toHaveLength(2);
  expect(screen.getAllByText('The judging AI was not sure.')).toHaveLength(3);

  await user.press(screen.getAllByText("Claude's sentence")[0]); // "Seoul is the capital."
  expect(await screen.findByText('Why')).toBeTruthy();
  expect(screen.getByText("OpenCode: Claude's sentence vs Codex's answer · Same · not sure")).toBeTruthy();
  expect(screen.getByText("Codex: Claude's sentence vs OpenCode's answer · Same")).toBeTruthy();
  expect(screen.getAllByText('The judging AI was not sure.')).toHaveLength(4); // the cards and the sheet
});

test('when one judge fails, the rest still show, the sheet says why, and Compare again fills in the rest', async () => {
  agent.judgeReplies.codex = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  await finished();
  for (const name of ['Differ 1', 'Agree 1', 'Only one 2']) expect(screen.getByRole('button', { name })).toBeTruthy();
  expect(screen.getByText('Could not compare with the answers of OpenCode.')).toBeTruthy(); // Claude's "Try the palace."
  expect(screen.getByText('Could not compare with the answers of Claude.')).toBeTruthy(); // OpenCode's "Eat at the market."

  await user.press(screen.getByRole('button', { name: /^Judged by: / }));
  expect(await screen.findByText('Judged by')).toBeTruthy();
  expect(screen.getAllByText(lost)).toHaveLength(2);
  expect(screen.getByText("Codex: Claude's sentences ↔ OpenCode's answer")).toBeTruthy();
  expect(screen.getByText("OpenCode: Claude's sentences ↔ Codex's answer")).toBeTruthy();
  expect(screen.getAllByText('9.8 s · deepseek-v4.1-flash')).toHaveLength(2);
  await user.press(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByText('Judged by')).toBeNull());

  agent.judgeReplies.codex = 'ok';
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await waitFor(() => expect(screen.queryByText(/^Could not compare/)).toBeNull());
  await finished();
  expect(agent.compareCalls('POST', '/retry')).toHaveLength(1);
  expect(screen.queryByText(/^Could not compare/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Only one 2' })).toBeTruthy();
});

test('when every judge fails, it says why in the AI words and Compare again judges again', async () => {
  for (const a of ['claude', 'codex', 'opencode']) agent.judgeReplies[a] = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText(lost, {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Differ/ })).toBeNull();

  agent.judgeReplies = {};
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await waitFor(() => expect(screen.queryByText(lost)).toBeNull());
  await finished();
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
  expect(agent.compareCalls('POST', '/retry')).toHaveLength(1);
});

test('a follow-up to one AI is not compared and has no Summary tab', async () => {
  const user = await open();
  await ask(user, 'first');
  await finished();
  agent.askReplies = {};
  await user.press(screen.getByRole('tab', { name: 'Codex' }));
  await user.press(screen.getByText('Only this AI'));
  await ask(user, 'only you', 'Follow up');
  expect(await screen.findByText('codex says: only you')).toBeTruthy();
  expect(agent.runRequests()[1].compare).toBeUndefined();
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);
  expect(screen.getAllByRole('tab', { name: 'Summary' })).toHaveLength(1);
  expect(agent.comparisonCount()).toBe(1);
});

test('asking two AIs only: no compare, and Summary says to turn one more on', async () => {
  const user = await open();
  await user.press(await screen.findByRole('switch', { name: 'OpenCode' })); // off
  await ask(user, 'q');
  expect(await screen.findByText(/^Only two AIs were asked, so there is no comparison\./)).toBeTruthy();
  expect(screen.getAllByRole('tab').map((x) => x.props.accessibilityLabel)).toEqual(['Summary', 'Claude', 'Codex']);
  await user.press(screen.getByRole('tab', { name: 'Codex' }));
  expect(await screen.findByText(answers.codex)).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
  expect(agent.comparisonCount()).toBe(0);
});

test('a PC with only two AIs that judge says comparing needs one more', async () => {
  agent.askAgents = agent.askAgents.filter((a) => a.agent === 'codex');
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText(/^This PC has only two AIs that can judge/)).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
});

test('an older agent that does not judge: no compare, no tabs, the P2-2 cards', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  expect(
    await screen.findByText('One question goes to every AI turned on, at once. Answers show as they come. Long-press an AI to pick its model.'),
  ).toBeTruthy();
  await ask(user, 'q');
  expect(await screen.findByText(answers.opencode)).toBeTruthy();
  expect(screen.getByLabelText("Claude's answer")).toBeTruthy();
  expect(screen.queryByRole('tab')).toBeNull();
  expect(screen.queryByText(/AIs answered/)).toBeNull();
  expect(agent.runRequests()[0].compare).toBeUndefined();
});

test('New question while comparing stops polling and asks the PC to stop', async () => {
  agent.enableJudge({ verified: true, typicalMs: 14000 });
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6')).toBeTruthy();
  expect(screen.getByText('It usually takes about 14 s.')).toBeTruthy();
  expect(screen.queryByText('The judging accuracy has not been measured yet.')).toBeNull();
  expect(screen.getByRole('button', { name: 'Check this …' })).toBeTruthy(); // filled once every judge ended
  const [id] = agent.comparisonIds();
  // renderRouter runs fake timers: time passes only when advanced.
  const later = () => act(() => jest.advanceTimersByTimeAsync(200));
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
  expect(screen.queryByText('Comparing… 3/6')).toBeNull();
});

test('a follow-up sends the earlier questions every judge answered, not one sent to one AI', async () => {
  agent.askReplies = {};
  const user = await open();
  await ask(user, 'first');
  await finished();
  await user.press(screen.getByRole('tab', { name: 'Codex' }));
  await user.press(screen.getByText('Only this AI'));
  await ask(user, 'only you', 'Follow up');
  await screen.findByText('codex says: only you');
  await ask(user, 'all again', 'Follow up');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].compare).toEqual({ earlier: ['first'], judges: ['claude', 'codex', 'opencode'] });
  await waitFor(() => expect(screen.getAllByRole('tab', { name: 'Summary' })).toHaveLength(2));
});

test('Settings turns auto compare off; then Compare sends the answers for a comparison', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/settings' });
  const sw = await screen.findByRole('switch', { name: 'Compare answers automatically' });
  expect(sw).toBeChecked();
  expect(screen.getByText(/^Once the answers are in, each AI judges twice more/)).toBeTruthy();
  await user.press(sw);
  await waitFor(() => expect(screen.getByRole('switch', { name: 'Compare answers automatically' })).not.toBeChecked());

  await renderRouter(appDir, { initialUrl: '/ask' });
  // With auto compare off, the intro does not promise a comparison.
  expect(await screen.findByText(/^One question goes to every AI turned on, at once\. Answers show as they come\. Long-press/)).toBeTruthy();
  await ask(user, 'q');
  expect(await screen.findByText("This uses the PC's AIs 6 more times.")).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
  expect(agent.comparisonCount()).toBe(0);

  await user.press(screen.getByRole('button', { name: 'Compare' }));
  await finished();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body).toEqual({
    clientComparisonId: expect.stringMatching(/^[0-9a-f-]{36}$/),
    question: 'q',
    earlier: [],
    judges: ['claude', 'codex', 'opencode'],
    answers: ['claude', 'codex', 'opencode'].map((a) => ({
      id: a,
      agent: a,
      via: 'pc',
      model: `${a}-standard`,
      text: answers[a as keyof typeof answers],
    })),
  });
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
  expect(agent.comparisonCount('direct')).toBe(1);
});

test('a refused Compare says why and offers Compare again with a new request', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('switch', { name: 'Compare answers automatically' })); // off
  await renderRouter(appDir, { initialUrl: '/ask' });
  agent.comparisonError = { status: 429, code: 'too_many_comparisons' };
  await ask(user, 'q');
  await user.press(await screen.findByRole('button', { name: 'Compare' }));
  expect(await screen.findByText('The PC is still comparing other answers. Tap Compare in a moment.')).toBeTruthy();

  agent.comparisonError = null;
  await user.press(screen.getByRole('button', { name: 'Compare' }));
  await finished();
  const keys = agent.callsTo('POST', '/v1/comparisons').map((c) => c.body.clientComparisonId);
  expect(keys).toHaveLength(2);
  expect(keys[1]).not.toBe(keys[0]); // the PC answered the first, so its key is done
  expect(agent.comparisonCount('direct')).toBe(1);
});

test('a comparison the PC lost (404) is made again from the answers', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  await screen.findByText('Comparing… 3/6');
  agent.forgetComparisons();
  expect(await screen.findByText(/^The PC no longer has this comparison/)).toBeTruthy();

  agent.compareHold = false;
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await finished();
  expect(agent.callsTo('POST', '/v1/comparisons')).toHaveLength(1);
  const [first, second] = agent.comparisonIds();
  expect(second).not.toBe(first);
  expect(agent.compareCalls('GET', `/${second}`).length).toBeGreaterThan(0);
  expect(screen.queryByText(/^The PC no longer has this comparison/)).toBeNull();
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
});

test('a lost POST /v1/runs: Try again sends the same run, and it is compared once', async () => {
  agent.loseReplies['POST /v1/runs'] = 3;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText(/^Can't reach the PC/, {}, { timeout: 6000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  await finished();
  const ids = agent.runRequests().map((r) => r.clientRunId);
  expect(ids).toHaveLength(4);
  expect(new Set(ids).size).toBe(1);
  expect(agent.comparisonCount()).toBe(1);
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
});

test('a lost POST /v1/comparisons: Try again sends the same clientComparisonId, one comparison', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  await screen.findByText('Comparing… 3/6');
  agent.forgetComparisons();
  await screen.findByText(/^The PC no longer has this comparison/);
  agent.compareHold = false;
  agent.loseReplies['POST /v1/comparisons'] = 3;
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await screen.findByText(/^Can't reach the PC/, {}, { timeout: 6000 })).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Try again' }));
  await finished();
  const keys = agent.callsTo('POST', '/v1/comparisons').map((c) => c.body.clientComparisonId);
  expect(keys).toHaveLength(4);
  expect(new Set(keys).size).toBe(1);
  expect(agent.comparisonCount('direct')).toBe(1);
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
});

test('after a 10 KiB question the follow-up sends it cut to 4 KiB on a character boundary', async () => {
  agent.askReplies = {};
  const user = await open();
  const big = '가'.repeat(1365) + '😀' + '가'.repeat(2000);
  await user.paste(await screen.findByLabelText('Ask a question'), big);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await finished();
  await ask(user, 'next', 'Follow up');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  const earlier = agent.runRequests()[1].compare.earlier;
  expect(earlier).toEqual(['가'.repeat(1365)]); // 4,095 bytes; the emoji would make 4,099
  expect(agent.compareErrors).toEqual([]);
  expect(agent.comparisonCount()).toBe(2);
});

test('in Korean the Summary tab reads in Korean', async () => {
  setLang('ko');
  const user = await open();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  await finished('한 AI만');
  expect(screen.getByRole('tab', { name: '정리' })).toBeSelected();
  expect(screen.getByText('AI 3개가 답했습니다 · 내 PC 3')).toBeTruthy();
  expect(screen.getByRole('button', { name: '다른 말 1' })).toBeTruthy();
  expect(cardHeads(['다른 말', '확인 필요', '같은 말', '한 AI만'])).toEqual(['다른 말', '같은 말', '한 AI만']);
  expect(screen.getByText('AI끼리 서로 다르게 말한 곳입니다. 직접 확인하세요.')).toBeTruthy();
  expect(screen.getByRole('button', { name: '두 문장 나란히 보기 ›' })).toBeTruthy();
  expect(screen.getByText('판정: Claude haiku · Codex gpt-6-luna · OpenCode deepseek-v4.1-flash ›')).toBeTruthy();
  expect(screen.getByText('비교도 AI가 한 판정이라 틀릴 수 있습니다.')).toBeTruthy();
  expect(screen.getByText('Claude만')).toBeTruthy();
});

test('item keys stay while judges finish: an open sheet stays on the same sentences', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6')).toBeTruthy();
  expect(screen.getByText('Claude ↔ Codex, OpenCode')).toBeTruthy(); // Differ shows at once
  expect(cardHeads()).toEqual(['Differ']);
  await user.press(screen.getByRole('button', { name: 'See both sentences ›' }));
  expect(await screen.findByText('Judgments by direction')).toBeTruthy();
  expect(screen.getByText("OpenCode: Claude's sentence vs Codex's answer · Differs")).toBeTruthy();
  expect(screen.queryByText("Claude: Codex's sentence vs OpenCode's answer · Same")).toBeNull(); // not judged yet

  agent.compareHold = false;
  await finished();
  expect(screen.getByText('Judgments by direction')).toBeTruthy();
  expect(screen.getByText("Claude: Codex's sentence vs OpenCode's answer · Same")).toBeTruthy();
  expect(screen.getAllByText('It has 9.7 million people.')).toHaveLength(2);
  expect(cardHeads()).toEqual(['Differ', 'Agree', 'Only one']);
});

test('two AIs that failed to answer leave nothing to compare', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.askReplies.opencode = { code: 'ask_timeout' };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Only one AI answered, so there is nothing to compare.')).toBeTruthy();
  expect(screen.getByText('Codex ✕')).toBeTruthy();
  expect(screen.getByText('OpenCode ✕')).toBeTruthy();
});

test('a long answer says only its first sentences were compared', async () => {
  agent.maxUnits = 2;
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(screen.getByText("Claude's answer is long: only its first 2 sentences were compared.")).toBeTruthy();
  expect(screen.getByText("OpenCode's answer is long: only its first 2 sentences were compared.")).toBeTruthy();
  expect(screen.queryByText(/^Codex's answer is long/)).toBeNull();
});

test('no key sentences and no conflicts: it says so instead of empty cards', async () => {
  agent.core = false;
  agent.askReplies = {};
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(screen.getByText('Found no key sentences to compare.')).toBeTruthy();
  expect(cardHeads()).toEqual([]);
  expect(screen.queryByText('Found no place where the AIs said different things.')).toBeNull();
});

test('math shows only where the PC marked it; a card shows 3 items, then more', async () => {
  agent.askReplies = {
    claude: 'The area is $\\pi r^2$ here. Alpha one. Beta two. Gamma three.',
    codex: 'Run `$HOME/bin:$PATH` first.',
    opencode: 'Something else.',
  };
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(screen.getByText('The area is πr² here.')).toBeTruthy();
  expect(screen.queryByText(/\\pi/)).toBeNull();

  // Six Only one items: three, then the rest on request.
  expect(screen.getByRole('button', { name: 'Only one 6' })).toBeTruthy();
  expect(screen.queryByText('Run $HOME/bin:$PATH first.')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Show 3 more' }));
  expect(screen.getByText('Run $HOME/bin:$PATH first.')).toBeTruthy(); // code, so no math span
  expect(screen.queryByRole('button', { name: 'Show 3 more' })).toBeNull();
  expect(screen.getByText('Found no place where the AIs said different things.')).toBeTruthy();
});

// ---- Found in the S5 review (R-34) ----

test('the PC stops answering polls: Try again follows the same comparison, no new one', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6')).toBeTruthy();
  agent.reach = 'offline';
  // 30 dropped looks in a row, a second apart, end the follow.
  await act(() => jest.advanceTimersByTimeAsync(35000));
  expect(await screen.findByText(/^Can't reach the PC/)).toBeTruthy();
  agent.reach = 'ok';
  agent.compareHold = false;
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  await finished();
  expect(screen.getByRole('button', { name: 'Differ 1' })).toBeTruthy();
  expect(agent.callsTo('POST', '/v1/comparisons')).toHaveLength(0);
  expect(agent.comparisonCount()).toBe(1);
});

test('a partial comparison with no Differ says only the compared sentences agree', async () => {
  agent.askReplies = {
    claude: 'Seoul is the capital. Try the palace.',
    codex: 'Seoul is the capital.',
    opencode: 'Seoul is the capital. Eat at the market.',
  };
  agent.judgeReplies.codex = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(screen.getByRole('button', { name: 'Differ 0' })).toBeTruthy();
  expect(
    screen.getByText(
      'In the sentences that were compared, found no place where the AIs said different things. Could not compare with the answers of Claude, OpenCode.',
    ),
  ).toBeTruthy();
  expect(screen.queryByText('Found no place where the AIs said different things.')).toBeNull();
});

test('when no AI answered, Summary does not say one did', async () => {
  agent.askReplies = {
    claude: { code: 'ask_failed', message: 'x' },
    codex: { code: 'ask_failed', message: 'y' },
    opencode: { code: 'ask_timeout' },
  };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing needs at least two answers.', {}, { timeout: 5000 })).toBeTruthy();
  for (const name of ['Claude ✕', 'Codex ✕', 'OpenCode ✕']) expect(screen.getByText(name)).toBeTruthy();
  expect(screen.queryByText('Only one AI answered, so there is nothing to compare.')).toBeNull();
  expect(screen.queryByText(/^AIs answered/)).toBeNull();
});

test('when no pair had a judge, the footer names nobody and has no link', async () => {
  agent.askReplies.opencode = { code: 'ask_timeout' };
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'q');
  await screen.findByText('AIs answered: 1 · My PC 1');
  // OpenCode can no longer judge by the time the PC plans the comparison.
  agent.askAgents = agent.askAgents.map((a) => (a.agent === 'opencode' ? { ...a, capabilities: a.capabilities.filter((c) => c !== 'judge') } : a));
  agent.runLimit = Infinity;
  expect(
    await screen.findByText('No AI could judge: it takes an AI of another company that did not write the answers.', {}, { timeout: 5000 }),
  ).toBeTruthy();
  expect(screen.queryByText(/^Judged by:/)).toBeNull();
  expect(screen.queryByRole('button', { name: /^Judged by/ })).toBeNull();
  expect(screen.getByText("The judging AI's company receives the question and the other AIs' answers.")).toBeTruthy();
});

test('a tab tells a screen reader what its mark shows', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'q');
  await screen.findByText('AIs answered: 1 · My PC 1');
  const tab = (name: string) => screen.getByRole('tab', { name });
  expect(tab('Codex')).toHaveAccessibilityValue({ text: 'Answering…' });
  expect(tab('Codex')).toBeBusy();
  expect(tab('Claude')).not.toBeBusy();
  expect(tab('Claude')).not.toHaveAccessibilityValue({ text: /./ });

  agent.runLimit = Infinity;
  expect(await screen.findByText('Codex ✕', {}, { timeout: 5000 })).toBeTruthy();
  expect(tab('Codex')).toHaveAccessibilityValue({ text: 'Did not answer' });
  expect(tab('Codex')).not.toBeBusy();
  expect(tab('Summary')).toBeSelected();
  expect(tab('Summary')).not.toHaveAccessibilityValue({ text: /./ });
});

test('while comparing, a picked card with nothing yet says it is still comparing', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6')).toBeTruthy();
  expect(screen.queryByText('Comparing…')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Agree …' }));
  expect(cardHeads()).toEqual(['Agree']);
  expect(screen.getByText('Two or more AIs said the same thing. That does not mean it is right.')).toBeTruthy();
  expect(screen.getByText('Comparing…')).toBeTruthy();

  agent.compareHold = false;
  await finished();
  expect(screen.getByRole('button', { name: 'Agree 1' })).toBeSelected();
  expect(screen.getByText('Seoul is the capital.')).toBeTruthy();
  expect(screen.queryByText('Comparing…')).toBeNull();
  // Once every judge ended, a picked empty card is just empty (S5 decision 9).
  await user.press(screen.getByRole('button', { name: 'Check this 0' }));
  expect(cardHeads()).toEqual(['Check this']);
  expect(screen.queryByText('Comparing…')).toBeNull();
});

// WCAG 1.4.11: a control's parts stand out 3:1 from what is around them.
const luminance = (hex: string) => {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a: string, b: string) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

test.each([
  ['light', '#f6f6f4'],
  ['dark', '#141413'],
] as const)('the Settings switch stands out when it is off (%s)', async (scheme, page) => {
  jest.spyOn(ReactNative, 'useColorScheme').mockReturnValue(scheme);
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('switch', { name: 'Compare answers automatically' }));
  const sw = screen.getByRole('switch', { name: 'Compare answers automatically' });
  expect(sw).not.toBeChecked();
  type El = Exclude<(typeof sw.children)[number], string>;
  const [, track] = sw.children.filter((c): c is El => typeof c !== 'string'); // the label, then the track
  const [knob] = track.children.filter((c): c is El => typeof c !== 'string');
  const colour = (el: El) => StyleSheet.flatten(el.props.style).backgroundColor as string;
  expect(contrast(colour(track), page)).toBeGreaterThanOrEqual(3);
  expect(contrast(colour(knob), colour(track))).toBeGreaterThanOrEqual(3);
});
