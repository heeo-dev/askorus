// P2-3 / S5c: a question compared across the AIs on 한 번에, against a
// pretend agent whose AIs judge each other's answers (fakeAgent
// enableJudge, fakeCompare) but write no combined answer: who is answering,
// the comparison's step, the evidence (all four sections), the pair sheet,
// the judging details, failures and retries, lost requests, follow-ups,
// and Korean. Every user event is awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

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
const lost = 'The judging AI reported an error.\nusage limit reached';
const differItem = /^It has 9\.4 million people\. Claude ↔ Codex, OpenCode 9\.7/;

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

async function follow(user: User, question: string, field = 'Follow up', send = 'Send') {
  await user.type(screen.getByLabelText(field), question);
  await user.press(screen.getByRole('button', { name: send }));
}

// The count on an evidence section of the latest turn ('…' while judging).
const count = (kind: string) => {
  const secs = screen.getAllByTestId(`evidence-${kind}`);
  return String(within(secs[secs.length - 1]).getAllByText(/^(\d+|…)$/)[0].props.children);
};
// The comparison of the latest turn is final once Only one has a number.
const finished = () => waitFor(() => expect(count('onlyOne')).toMatch(/^\d+$/), { timeout: 5000 });
const counts = () => ['differ', 'check', 'agree', 'onlyOne'].map(count);
const sheet = (id: string) => screen.getByTestId(id);
const close = () => screen.getByRole('button', { name: 'Close' });
// renderRouter runs fake timers: time passes only when advanced.
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('while the AIs answer: who is at it, their models without the variant, and the follow-up waits', async () => {
  agent.runLimit = 1;
  const user = await open({ models: { opencode: 'opencode-go/deepseek-v4.1-flash#high' } });
  expect(await screen.findByText(/^One question goes to every AI turned on, at once\. Once the answers are in/)).toBeTruthy();
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
  expect(screen.getByText('Once every answer is in, they are compared.')).toBeTruthy();
  // A follow-up written while they answer waits (TEST-1: with text in the box, so only the wait disables it).
  await user.type(screen.getByLabelText('Follow up'), 'next');
  const send = screen.getByRole('button', { name: 'Send' });
  expect(send).toBeDisabled();
  expect(send.props.accessibilityHint).toBe('You can send once every answer is in.');
  expect(agent.runRequests()[0].agents[2]).toEqual({ agent: 'opencode', model: 'opencode-go/deepseek-v4.1-flash#high' });

  agent.runLimit = Infinity;
  await finished();
  expect(screen.queryByRole('header', { name: 'Answering…' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled(); // the follow-up kept, ready to go
  expect(screen.getByRole('header', { name: 'Comparison' })).toBeTruthy();
  expect(screen.queryByText(/#high/)).toBeNull();
});

test('while comparing: step one with its count and usual time; the evidence fills as judges end', async () => {
  agent.enableJudge({ verified: true, typicalMs: 40444 });
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByText('usually 40 s')).toBeTruthy();
  expect(screen.getByLabelText('Comparing… 3/6 usually 40 s')).toBeTruthy();
  // No combined answer here: one step, no grey lines, the evidence open.
  expect(screen.queryByLabelText(/^Next step/)).toBeNull();
  expect(screen.queryByTestId('skeleton', { includeHiddenElements: true })).toBeNull();
  expect(counts()).toEqual(['1', '…', '…', '…']);
  expect(within(sheet('evidence-agree')).getByText('Comparing…')).toBeTruthy();
  expect(screen.getByRole('button', { name: differItem })).toBeTruthy(); // Differ shows at once

  agent.compareHold = false;
  await finished();
  expect(screen.queryByText(/^Comparing…/)).toBeNull();
  expect(counts()).toEqual(['1', '0', '1', '2']);
});

test('the evidence: all four sections, Differ with the other sides’ numbers, an empty one says so, fold and open', async () => {
  const user = await open();
  await ask(user, 'Tell me about Seoul');
  await finished();
  expect(screen.getByRole('header', { name: 'Evidence' })).toBeTruthy();
  for (const [kind, caption] of [
    ['differ', 'The AIs said different things'],
    ['check', 'Check these yourself'],
    ['agree', 'That does not make it right'],
    ['onlyOne', 'It may be right or wrong'],
  ])
    expect(within(sheet(`evidence-${kind}`)).getByText(caption)).toBeTruthy();
  // Differ: Claude's sentence, the digits bold, and what the other side says.
  const differ = within(sheet('evidence-differ'));
  expect(differ.getByText('It has 9.4 million people.')).toBeTruthy();
  expect(differ.getByText('9.4')).toHaveStyle({ fontWeight: '700' });
  expect(differ.getByText('Claude ↔ Codex, OpenCode 9.7')).toBeTruthy();
  expect(differ.getByText('9.7')).toHaveStyle({ fontWeight: '600' });
  // R-42: an empty section says there is nothing in it.
  expect(within(sheet('evidence-check')).getByText('No sentences here.')).toBeTruthy();
  expect(within(sheet('evidence-agree')).getByText('Seoul is the capital.')).toBeTruthy();
  expect(within(sheet('evidence-agree')).getByText('Claude · Codex · OpenCode')).toBeTruthy();
  expect(within(sheet('evidence-onlyOne')).getByText('Only Claude')).toBeTruthy();
  expect(within(sheet('evidence-onlyOne')).getByText('Only OpenCode')).toBeTruthy();

  // Hide: the four counts; one of them opens the sections again.
  await user.press(screen.getByRole('button', { name: 'Hide' }));
  expect(screen.queryByTestId('evidence-differ')).toBeNull();
  for (const name of ['Differ: 1. Show', 'Check this: 0. Show', 'Agree: 1. Show', 'Only one: 2. Show']) expect(screen.getByRole('button', { name })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Agree: 1. Show' }));
  expect(screen.getByTestId('evidence-differ')).toBeTruthy();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'] });
});

test('a Differ item opens the pair sheet: each side, one line per direction, and each answer', async () => {
  const user = await open();
  await ask(user, 'q');
  await finished();
  await user.press(screen.getByRole('button', { name: differItem }));
  const pair = within(await screen.findByTestId('pair-sheet'));
  expect(pair.getByRole('header', { name: 'Differ' })).toBeTruthy();
  expect(pair.getByText('Judgments by direction')).toBeTruthy();
  expect(pair.getByText('It has 9.4 million people.')).toBeTruthy();
  expect(pair.getAllByText('It has 9.7 million people.')).toHaveLength(2); // Codex's and OpenCode's, each named
  expect(pair.getByText('Codex, OpenCode')).toBeTruthy();
  // On a side of two AIs each sentence carries its AI's name (TEST-4); a side of one does not repeat it.
  for (const name of ['Codex', 'OpenCode']) expect(within(pair.getByText(name).parent as never).getByText('It has 9.7 million people.')).toBeTruthy();
  expect(pair.getAllByText('Claude')).toHaveLength(1); // its side's name only
  for (const d of pair.getAllByText('9.7')) expect(d).toHaveStyle({ fontWeight: '700' });
  for (const line of [
    "OpenCode: Claude's sentence vs Codex's answer · Differs",
    "OpenCode: Codex's sentence vs Claude's answer · Differs",
    "Codex: Claude's sentence vs OpenCode's answer · Differs",
    "Codex: OpenCode's sentence vs Claude's answer · Differs",
    "Claude: Codex's sentence vs OpenCode's answer · Same",
    "Claude: OpenCode's sentence vs Codex's answer · Same",
  ])
    expect(pair.getByText(line)).toBeTruthy();
  expect(pair.queryByText(/%/)).toBeNull(); // no probabilities

  await user.press(pair.getByRole('button', { name: "See Codex's answer" }));
  const answer = within(await screen.findByTestId('answer-sheet'));
  expect(screen.queryByTestId('pair-sheet')).toBeNull();
  expect(answer.getByRole('header', { name: 'Codex' })).toBeTruthy();
  expect(answer.getByText(answers.codex)).toBeTruthy();
  expect(answer.getByText('My PC · Judged by: Claude, OpenCode')).toBeTruthy();
});

test('two sentences of one AI on a side: each direction is still one line (R-41)', async () => {
  // The phone check: the pair sheet repeated a direction's line for each sentence.
  agent.askReplies.claude = 'It has 9.4 million people. Seoul is big. It has 9.4 million people.';
  const user = await open();
  await ask(user, 'q');
  await finished();
  await user.press(screen.getByRole('button', { name: differItem }));
  const pair = within(await screen.findByTestId('pair-sheet'));
  expect(pair.getAllByText('It has 9.4 million people.')).toHaveLength(2);
  expect(pair.getByText("OpenCode: Claude's sentence vs Codex's answer · Differs ×2")).toBeTruthy();
  expect(pair.getByText("Codex: Claude's sentence vs OpenCode's answer · Differs ×2")).toBeTruthy();
  expect(pair.getAllByText(/^OpenCode: Claude's sentence vs Codex's answer/)).toHaveLength(1);
});

test('a judge that is not sure puts sentences under Check this; tapping one says why', async () => {
  agent.judgeReplies.opencode = 'unsure';
  const user = await open();
  await ask(user, 'q');
  await finished();
  // OpenCode was not sure of Claude's and Codex's sentences; OpenCode's own 'Eat at the market.' is the one Only one.
  expect(counts()).toEqual(['1', '3', '0', '1']);
  const check = within(sheet('evidence-check'));
  expect(check.getAllByText("Claude's sentence · The judging AI was not sure.")).toHaveLength(2);
  expect(within(sheet('evidence-agree')).getByText('No sentences here.')).toBeTruthy();

  await user.press(screen.getByRole('button', { name: "Seoul is the capital. Claude's sentence · The judging AI was not sure." }));
  const pair = within(await screen.findByTestId('pair-sheet'));
  expect(pair.getByText('Why')).toBeTruthy();
  expect(pair.getByText('The judging AI was not sure.')).toBeTruthy();
  expect(pair.getByText("OpenCode: Claude's sentence vs Codex's answer · Same · not sure")).toBeTruthy();
  expect(pair.getByText("Codex: Claude's sentence vs OpenCode's answer · Same")).toBeTruthy();
});

test('when one judge fails, the rest still show, the judging details say why, and Compare again fills in the rest', async () => {
  agent.judgeReplies.codex = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(counts()).toEqual(['1', '0', '1', '2']);
  expect(screen.getByText('Could not compare with the answers of OpenCode.')).toBeTruthy(); // Claude's "Try the palace."
  expect(screen.getByText('Could not compare with the answers of Claude.')).toBeTruthy(); // OpenCode's "Eat at the market."

  await user.press(screen.getAllByRole('button', { name: 'Judging details' })[0]);
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByRole('header', { name: 'Judging details' })).toBeTruthy();
  expect(judges.getByText('Judgments: 6')).toBeTruthy();
  // #3: what a judgment is, under the count.
  expect(judges.getByText(/^Each judgment checks one AI's sentences against another AI's answer/)).toBeTruthy();
  expect(judges.getByRole('header', { name: /^Judged by Codex(, |$)/ })).toBeTruthy();
  expect(judges.getAllByText(lost)).toHaveLength(2);
  expect(judges.getByLabelText(`Claude's sentences against OpenCode's answer, ${lost}`)).toBeTruthy();
  expect(judges.getByRole('header', { name: /^Judged by OpenCode(, |$)/ })).toBeTruthy();
  expect(judges.getAllByText('9.8 s')).toHaveLength(4);
  expect(judges.getByText('deepseek-v4.1-flash')).toBeTruthy();
  // The notes that were under the answer: companies, can be wrong, not measured.
  expect(judges.getByText("The judging AI's company receives the question and the other AIs' answers.")).toBeTruthy();
  expect(judges.getByText("The comparison is an AI's judgment too, and it can be wrong.")).toBeTruthy();
  expect(judges.getByText('The judging accuracy has not been measured yet.')).toBeTruthy();
  expect(judges.queryByText(/^An AI wrote this combined answer/)).toBeNull();
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('judges-sheet')).toBeNull());

  agent.judgeReplies.codex = 'ok';
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await waitFor(() => expect(screen.queryByText(/^Could not compare/)).toBeNull());
  await finished();
  expect(agent.compareCalls('POST', '/retry')).toHaveLength(1);
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();
  expect(counts()).toEqual(['1', '0', '1', '2']);
});

test('when every judge fails, it says why in the AI words and Compare again judges again', async () => {
  for (const a of ['claude', 'codex', 'opencode']) agent.judgeReplies[a] = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText(lost, {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByTestId('evidence-differ')).toBeNull();
  expect(screen.queryByRole('button', { name: /^Differ: / })).toBeNull();

  agent.judgeReplies = {};
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  await waitFor(() => expect(screen.queryByText(lost)).toBeNull());
  await finished();
  expect(count('differ')).toBe('1');
  expect(agent.compareCalls('POST', '/retry')).toHaveLength(1);
});

test('a follow-up to one AI is not compared: its answer in full', async () => {
  const user = await open();
  await ask(user, 'first');
  await finished();
  agent.askReplies = {};
  await user.press(screen.getByRole('button', { name: /^Codex's answer\./ }));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  await waitFor(() => expect(screen.queryByTestId('answer-sheet')).toBeNull());
  await follow(user, 'only you', 'Ask Codex a follow-up', 'Send to Codex');
  expect(await screen.findByText('codex says: only you')).toBeTruthy();
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
  expect(agent.runRequests()[1].compare).toBeUndefined();
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);
  expect(screen.getAllByRole('header', { name: 'Comparison' })).toHaveLength(1);
  expect(agent.comparisonCount()).toBe(1);
});

test('asking two AIs only: no comparison, each answer in full, and why', async () => {
  const user = await open({ off: ['opencode'] });
  await ask(user, 'q');
  expect(await screen.findByText(/^Only two AIs were asked, so there is no comparison\./)).toBeTruthy();
  expect(await screen.findByText(answers.codex)).toBeTruthy();
  expect(screen.getByLabelText("Claude's answer")).toBeTruthy();
  expect(screen.queryByRole('header', { name: 'Comparison' })).toBeNull();
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

test('an older agent that does not judge: each answer in full, the plain intro', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  await ask(user, 'q');
  expect(await screen.findByText(answers.opencode)).toBeTruthy();
  expect(screen.getByLabelText("Claude's answer")).toBeTruthy();
  expect(screen.queryByRole('header', { name: 'Comparison' })).toBeNull();
  expect(agent.runRequests()[0].compare).toBeUndefined();
});

test('New question while comparing stops polling and asks the PC to stop, once', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
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
  expect(screen.queryByText('Comparing… 3/6')).toBeNull();
});

test('a conversation left while comparing is looked at again once when reopened', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'New question' }));
  await waitFor(() => expect(agent.compareCalls('POST', '/cancel')).toHaveLength(1));
  agent.compareHold = false; // the requests already running end

  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'q' }));
  // The PC dropped the queued requests: the comparison ends, it does not spin.
  await finished();
  expect(screen.queryByText(/^Comparing…/)).toBeNull();
  expect(agent.comparisonCount()).toBe(1);
});

test('a follow-up sends the earlier questions every judge answered, not one sent to one AI', async () => {
  agent.askReplies = {};
  const user = await open();
  await ask(user, 'first');
  await finished();
  await user.press(screen.getByRole('button', { name: /^Codex's answer\./ }));
  await user.press(within(await screen.findByTestId('answer-sheet')).getByRole('button', { name: 'Only this AI' }));
  await follow(user, 'only you', 'Ask Codex a follow-up', 'Send to Codex');
  await screen.findByText('codex says: only you');
  await follow(user, 'all again');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].compare).toEqual({ earlier: ['first'], judges: ['claude', 'codex', 'opencode'] });
  await waitFor(() => expect(screen.getAllByRole('header', { name: 'Comparison' })).toHaveLength(2), { timeout: 5000 });
});

test('auto compare off: Compare sends the answers for a comparison, with its cost', async () => {
  const user = await open({ compare: false });
  // The intro does not promise a comparison.
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
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
    answers: ['claude', 'codex', 'opencode'].map((a) => ({ id: a, agent: a, via: 'pc', model: `${a}-standard`, text: answers[a as keyof typeof answers] })),
  });
  expect(count('differ')).toBe('1');
  expect(agent.comparisonCount('direct')).toBe(1);
  expect(screen.queryByRole('button', { name: 'Compare' })).toBeNull();
});

test('a refused Compare says why and offers Compare again with a new request', async () => {
  const user = await open({ compare: false });
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
  await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 });
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
  expect(count('differ')).toBe('1');
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
  expect(count('differ')).toBe('1');
});

test('a lost POST /v1/comparisons: Try again sends the same clientComparisonId, one comparison', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 });
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
});

test('the PC stops answering polls: Try again follows the same comparison, no new one', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  agent.reach = 'offline';
  // 30 dropped looks in a row, a second apart, end the follow.
  await act(() => jest.advanceTimersByTimeAsync(35000));
  expect(await screen.findByText(/^Can't reach the PC/)).toBeTruthy();
  agent.reach = 'ok';
  agent.compareHold = false;
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  await finished();
  expect(count('differ')).toBe('1');
  expect(agent.callsTo('POST', '/v1/comparisons')).toHaveLength(0);
  expect(agent.comparisonCount()).toBe(1);
});

test('after a 10 KiB question the follow-up sends it cut to 4 KiB on a character boundary', async () => {
  agent.askReplies = {};
  const user = await open();
  const big = '가'.repeat(1365) + '😀' + '가'.repeat(2000);
  await user.paste(await screen.findByLabelText('Ask a question'), big);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await finished();
  await follow(user, 'next');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1].compare.earlier).toEqual(['가'.repeat(1365)]); // 4,095 bytes; the emoji would make 4,099
  expect(agent.compareErrors).toEqual([]);
  expect(agent.comparisonCount()).toBe(2);
});

test('item keys stay while judges finish: an open pair sheet stays on the same sentences', async () => {
  agent.compareHold = true;
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Comparing… 3/6', {}, { timeout: 5000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: differItem }));
  const pair = within(await screen.findByTestId('pair-sheet'));
  expect(pair.getByText("OpenCode: Claude's sentence vs Codex's answer · Differs")).toBeTruthy();
  expect(pair.queryByText("Claude: Codex's sentence vs OpenCode's answer · Same")).toBeNull(); // not judged yet

  agent.compareHold = false;
  await waitFor(() => expect(pair.getByText("Claude: Codex's sentence vs OpenCode's answer · Same")).toBeTruthy(), { timeout: 5000 });
  expect(screen.getByTestId('pair-sheet')).toBeTruthy();
  expect(pair.getAllByText('It has 9.7 million people.')).toHaveLength(2);
  await user.press(close());
  await finished();
  expect(counts()).toEqual(['1', '0', '1', '2']);
});

test('two AIs that failed to answer leave nothing to compare; with none, it does not say one did', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.askReplies.opencode = { code: 'ask_timeout' };
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('Only one AI answered, so there is nothing to compare.', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByRole('button', { name: /^Codex's answer\. codex-standard\. The AI reported an error\./ })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'AIs that answered: 1 (Claude). Details' })).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'New question' }));
  agent.askReplies.claude = { code: 'ask_failed', message: 'x' };
  await ask(user, 'q2');
  expect(await screen.findByText('Comparing needs at least two answers.', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByText('Only one AI answered, so there is nothing to compare.')).toBeNull();
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

test('no sentence judged at all: Differ says none could be compared, the others are empty', async () => {
  agent.maxUnits = 0;
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(within(sheet('evidence-differ')).getByText('Found no sentences to compare.')).toBeTruthy();
  expect(screen.getAllByText('No sentences here.')).toHaveLength(3);
  expect(screen.queryByText('Found no place where the AIs said different things.')).toBeNull();
});

test('sentences no judge called core still show on the cards (R-44)', async () => {
  // The phone check: Agree 0 and Only one 0 while the combined answer named 3 and 6, because
  // sentences no judge called core were hidden. The pretend agent now shows every judged one.
  agent.core = false;
  const user = await open();
  await ask(user, 'q');
  await finished();
  expect(counts()).toEqual(['1', '0', '1', '2']);
  expect(within(sheet('evidence-agree')).getByText('Seoul is the capital.')).toBeTruthy();
  expect(within(sheet('evidence-onlyOne')).getByText('Try the palace.')).toBeTruthy();
});

test('math shows only where the PC marked it; a section shows 3 items, then more', async () => {
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
  expect(count('onlyOne')).toBe('6');
  const only = () => within(sheet('evidence-onlyOne'));
  expect(only().queryByText('Run $HOME/bin:$PATH first.')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Show 3 more' }));
  expect(only().getByText('Run $HOME/bin:$PATH first.')).toBeTruthy(); // code, so no math span
  expect(screen.queryByRole('button', { name: 'Show 3 more' })).toBeNull();
  expect(screen.getByText('Found no place where the AIs said different things.')).toBeTruthy();
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
  expect(count('differ')).toBe('0');
  expect(
    screen.getByText(
      'In the sentences that were compared, found no place where the AIs said different things. Could not compare with the answers of Claude, OpenCode.',
    ),
  ).toBeTruthy();
  expect(screen.queryByText('Found no place where the AIs said different things.')).toBeNull();
});

test('when no pair had a judge, it says so and the judging details list the pairs', async () => {
  agent.askReplies.opencode = { code: 'ask_timeout' };
  agent.runLimit = 1;
  const user = await open();
  await ask(user, 'q');
  await waitFor(() => expect(screen.getByTestId('answering-claude')).toHaveAccessibilityValue({ text: 'Answered, 1 s' }));
  // OpenCode can no longer judge by the time the PC plans the comparison.
  agent.askAgents = agent.askAgents.map((a) => (a.agent === 'opencode' ? { ...a, capabilities: a.capabilities.filter((c) => c !== 'judge') } : a));
  agent.runLimit = Infinity;
  const why = 'No AI could judge: it takes an AI of another company that did not write the answers.';
  expect(await screen.findByText(why, {}, { timeout: 5000 })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Judging details' }));
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByText('Judgments: 0')).toBeTruthy();
  expect(judges.getByText('Claude ↔ Codex')).toBeTruthy();
  expect(judges.queryByRole('header', { name: /^Judged by / })).toBeNull();
  expect(judges.getByText("The judging AI's company receives the question and the other AIs' answers.")).toBeTruthy();
});

test('in Korean the comparison reads in Korean', async () => {
  setLang('ko');
  const user = await open();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  await finished();
  expect(screen.getByRole('header', { name: '비교' })).toBeTruthy();
  expect(screen.getByRole('header', { name: '근거' })).toBeTruthy();
  expect(within(sheet('evidence-differ')).getByText('AI끼리 다르게 말했습니다')).toBeTruthy();
  expect(within(sheet('evidence-differ')).getByText('Claude ↔ Codex, OpenCode 9.7')).toBeTruthy();
  expect(within(sheet('evidence-check')).getByText('해당하는 문장이 없습니다.')).toBeTruthy();
  expect(within(sheet('evidence-onlyOne')).getByText('Claude만')).toBeTruthy();
  expect(screen.getByRole('header', { name: 'AI별 답 3개' })).toBeTruthy();
  expect(screen.getByText(/^답 1초 · 비교 10초$/)).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '접기' }));
  expect(screen.getByRole('button', { name: '다른 말 1개, 펼치기' })).toBeTruthy();
  await user.press(screen.getAllByRole('button', { name: '판정 정보' })[0]);
  const judges = within(await screen.findByTestId('judges-sheet'));
  expect(judges.getByText('판정 6번')).toBeTruthy();
  expect(judges.getByRole('header', { name: /^Claude 판정(, |$)/ })).toBeTruthy();
  expect(judges.getByLabelText('Codex의 문장을 OpenCode의 답과, 9.8초')).toBeTruthy();
  expect(judges.getByText('비교도 AI가 한 판정이라 틀릴 수 있습니다.')).toBeTruthy();
});
