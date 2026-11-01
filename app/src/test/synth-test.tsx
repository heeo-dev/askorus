// P2-3 S5b: the combined answer at the top of the 정리 (Summary) tab, against
// a pretend agent whose AIs judge each other's answers and then one writes
// them into one (fakeAgent enableSynth, fakeCompare combinedText). Every
// user event is awaited (R-07).

import { act, userEvent } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

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
const differs = 'The AIs differ. Claude: It has 9.4 million people. Codex, OpenCode: It has 9.7 million people.';
const evidence = 'Show the evidence cards · Differ 1 · Check this 0 · Agree 1 · Only one 2';
const company = "The writing AI's company receives the question and every answer (with the AIs' names).";
const mayErr = 'An AI wrote this, so it can be wrong. Check where the AIs differ in the cards below.';

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

async function open(url = '/ask') {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: url });
  return user;
}

// Settings first, then the ask screen.
async function settingsThenAsk(change: (user: ReturnType<typeof userEvent.setup>) => Promise<void>) {
  const user = await open('/settings');
  await change(user);
  await renderRouter(appDir, { initialUrl: '/ask' });
  return user;
}

const written = () => screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
const chip = (name: string) => screen.queryByRole('button', { name });
// renderRouter runs fake timers: time passes only when advanced.
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('the combined answer comes on its own once compared; the cards fold under it as evidence', async () => {
  agent.compareHold = true;
  const user = await open();
  expect(await screen.findByText(/^Once the answers are compared, one AI writes a combined answer/)).toBeTruthy();
  await ask(user, 'Tell me about Seoul');

  // While the judges run: the card says it is coming, the cards are folded with '…'.
  expect(await screen.findByText('Comparing… 3/6')).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(screen.getByText('Combining…')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Show the evidence cards · Differ … · Check this … · Agree … · Only one …' })).toBeTruthy();
  expect(chip('Differ 1')).toBeNull();
  expect(screen.queryByText('Claude ↔ Codex, OpenCode')).toBeNull();

  // The judging ended, the writing goes on: no 다시 비교, the counts are in.
  agent.synthReply = 'hold';
  agent.compareHold = false;
  expect(await screen.findByRole('button', { name: evidence }, { timeout: 5000 })).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
  expect(screen.getByText('Combining…')).toBeTruthy();
  expect(screen.queryByText(/^Comparing/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();

  agent.synthReply = 'ok';
  expect(await written()).toBeTruthy();
  expect(screen.getByText(differs)).toBeTruthy();
  expect(screen.queryByText('Combining…')).toBeNull();
  expect(screen.getByText('Written by: Claude sonnet · 15.2 s ›')).toBeTruthy();
  expect(screen.getByText(mayErr)).toBeTruthy();
  expect(screen.queryByText(/^Written from the answers alone/)).toBeNull();
  expect(screen.queryByText(/^Some places where the AIs differ/)).toBeNull();
  expect(screen.getByText(company)).toBeTruthy();
  expect(screen.getByText("The judging AI's company receives the question and the other AIs' answers.")).toBeTruthy();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'], synthesis: { agent: 'claude' } });
  expect(agent.synthCalls).toBe(1);

  // Done: the polling stops.
  const gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);

  // The evidence opens and closes; the chips and cards are those of S5.
  expect(chip('Differ 1')).toBeNull();
  await user.press(screen.getByRole('button', { name: evidence }));
  expect(screen.getByRole('button', { name: 'Hide the evidence cards' })).toBeTruthy();
  for (const name of ['Differ 1', 'Check this 0', 'Agree 1', 'Only one 2']) expect(chip(name)).toBeTruthy();
  expect(screen.getByText('Claude ↔ Codex, OpenCode')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Hide the evidence cards' }));
  expect(chip('Differ 1')).toBeNull();
  expect(screen.getByRole('button', { name: evidence })).toBeTruthy();
});

test('Written by opens the sheet with the writer first; Copy copies the Markdown', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(screen.getByRole('button', { name: 'Written by: Claude sonnet · 15.2 s ›' }));
  expect(await screen.findByText('Judged by')).toBeTruthy();
  expect(screen.getByText('Combined answer by Claude')).toBeTruthy();
  expect(screen.getByText('15.2 s · sonnet')).toBeTruthy();
  expect(screen.getByText("OpenCode: Claude's sentences ↔ Codex's answer")).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByText('Judged by')).toBeNull());

  await user.press(screen.getByRole('button', { name: 'Copy' }));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(`Fake combined answer.\n\n${differs}`);
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('Settings picks the writing AI; the request names it and that AI writes', async () => {
  const user = await settingsThenAsk(async (u) => {
    expect(await screen.findByText('Combined answer')).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Write a combined answer once compared' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked();
    expect(screen.getByText(/^Uses one AI once more per question/)).toBeTruthy();
    await u.press(screen.getByRole('radio', { name: 'Codex' }));
    await waitFor(() => expect(screen.getByRole('radio', { name: 'Codex' })).toBeChecked());
    expect(screen.getByRole('radio', { name: 'Claude' })).not.toBeChecked();
  });
  await ask(user, 'q');
  await written();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ agent: 'codex' });
  expect(screen.getByText('Written by: Codex codex-std · 15.2 s ›')).toBeTruthy();
});

test('with the combined answer off: no request for one, the cards open, and Write one sends it', async () => {
  const user = await settingsThenAsk(async (u) => {
    await u.press(await screen.findByRole('radio', { name: 'OpenCode' }));
    await u.press(screen.getByRole('switch', { name: 'Write a combined answer once compared' }));
    await waitFor(() => expect(screen.getByRole('switch', { name: 'Write a combined answer once compared' })).not.toBeChecked());
  });
  // The intro does not promise one.
  expect(await screen.findByText(/^One question goes to every AI turned on/)).toBeTruthy();
  expect(screen.queryByText(/^Once the answers are compared, one AI writes/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByRole('button', { name: 'Write a combined answer' }, { timeout: 5000 })).toBeTruthy();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ off: true });
  expect(agent.synthCalls).toBe(0);
  expect(screen.getByText("This uses one of the PC's AIs 1 more time.")).toBeTruthy();
  // Off: the cards show open, as in S5, and nothing goes to a writer.
  for (const name of ['Differ 1', 'Agree 1', 'Only one 2']) expect(chip(name)).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Show the evidence cards/ })).toBeNull();
  expect(screen.queryByText(company)).toBeNull();
  expect(screen.queryByRole('header', { name: 'Combined answer' })).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Write a combined answer' }));
  expect(await written()).toBeTruthy();
  expect(agent.synthRequests()).toEqual([{ agent: 'opencode' }]);
  expect(agent.synthCalls).toBe(1);
  expect(screen.getByText('Written by: OpenCode opencode-std · 15.2 s ›')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Write a combined answer' })).toBeNull();
  expect(screen.getByRole('button', { name: evidence })).toBeTruthy();
  expect(screen.getByText(company)).toBeTruthy();
});

test('a writer that fails says why in its words; Write again writes it', async () => {
  agent.synthReply = 'fail';
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByText('The writing AI reported an error.\nusage limit reached', {}, { timeout: 5000 })).toBeTruthy();
  // Failed: the cards show open.
  for (const name of ['Differ 1', 'Agree 1', 'Only one 2']) expect(chip(name)).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Show the evidence cards/ })).toBeNull();
  // The sheet says it too.
  await user.press(screen.getByRole('button', { name: /^Judged by: / }));
  expect(await screen.findByText('Combined answer by Claude')).toBeTruthy();
  expect(screen.getAllByText('The writing AI reported an error.\nusage limit reached')).toHaveLength(2);
  await user.press(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByText('Judged by')).toBeNull());

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
  await ask(user, 'again', 'Follow up');
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(2), { timeout: 5000 });
  expect(screen.getByText('Some places where the AIs differ may be missing from the combined answer. Check the cards below.')).toBeTruthy();
  expect(screen.getAllByText('Written from the answers alone, without comparison results.')).toHaveLength(1);
});

test('when every judge fails, the combined answer is still written from the answers alone', async () => {
  for (const a of ['claude', 'codex', 'opencode']) agent.judgeReplies[a] = { code: 'judge_failed', message: 'usage limit reached' };
  const user = await open();
  await ask(user, 'q');
  await written();
  expect(screen.getByText('The judging AI reported an error.\nusage limit reached')).toBeTruthy();
  expect(screen.getByText('Written from the answers alone, without comparison results.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: /^Show the evidence cards/ })).toBeNull(); // no cards to fold
  // Once it is written, the judging may be tried again.
  expect(screen.getByRole('button', { name: 'Compare again' })).toBeTruthy();
});

test('an agent that does not write combined answers: the S5 Summary tab, nothing asked', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableJudge();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  const user = await open();
  expect(await screen.findByText(/the Summary tab shows where they differ and agree/)).toBeTruthy();
  expect(screen.queryByText(/^Once the answers are compared, one AI writes/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByRole('button', { name: 'Only one 2' }, { timeout: 5000 })).toBeTruthy();
  for (const name of ['Differ 1', 'Check this 0', 'Agree 1']) expect(chip(name)).toBeTruthy();
  expect(screen.getByText('Claude ↔ Codex, OpenCode')).toBeTruthy();
  expect(screen.queryByRole('header', { name: 'Combined answer' })).toBeNull();
  expect(screen.queryByRole('button', { name: /^Show the evidence cards/ })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Write a combined answer' })).toBeNull();
  expect(screen.queryByText(company)).toBeNull();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'] });
  expect(agent.synthRequests()).toEqual([]);
});

test('New question while the combined answer is written asks the PC to stop, once', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByRole('button', { name: evidence }, { timeout: 5000 })).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
  expect(screen.getByText('Combining…')).toBeTruthy();
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
  expect(screen.queryByText('Combining…')).toBeNull();
});

test('a comparison the PC lost is made again with the combined answer and the tier', async () => {
  agent.compareHold = true;
  const user = await open();
  await user.press(await screen.findByRole('radio', { name: 'Quick' }));
  await ask(user, 'q');
  await screen.findByText('Comparing… 3/6');
  agent.forgetComparisons();
  expect(await screen.findByText(/^The PC no longer has this comparison/)).toBeTruthy();

  agent.compareHold = false;
  await user.press(screen.getByRole('button', { name: 'Compare again' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body.synthesis).toEqual({ agent: 'claude', tier: 'quick' });
  expect(screen.getByText('Written by: Claude haiku · 15.2 s ›')).toBeTruthy();
  expect(agent.synthCalls).toBe(1);
});

test('Compare by hand costs one more for the combined answer and sends it', async () => {
  const user = await settingsThenAsk(async (u) => {
    await u.press(await screen.findByRole('switch', { name: 'Compare answers automatically' }));
    await waitFor(() => expect(screen.getByRole('switch', { name: 'Compare answers automatically' })).not.toBeChecked());
  });
  await ask(user, 'q');
  expect(await screen.findByText("This uses the PC's AIs 7 more times.")).toBeTruthy();
  expect(agent.runRequests()[0].compare).toBeUndefined();
  // Before the comparison the footer already says where the answers go.
  expect(screen.getByText(company)).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Compare' }));
  expect(await written()).toBeTruthy();
  const [post] = agent.callsTo('POST', '/v1/comparisons');
  expect(post.body.synthesis).toEqual({ agent: 'claude', tier: 'standard' });
  expect(agent.comparisonCount('direct')).toBe(1);
  expect(screen.getByText(differs)).toBeTruthy();
});

test('in Korean the combined answer reads in Korean', async () => {
  setLang('ko');
  const user = await open();
  expect(await screen.findByText(t('ask.introSynth'))).toBeTruthy();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  await written();
  expect(screen.getByRole('header', { name: '종합 답변' })).toBeTruthy();
  expect(screen.getByText('정리: Claude sonnet · 15.2초 ›')).toBeTruthy();
  expect(screen.getByText('AI가 정리한 글이라 틀릴 수 있습니다. 갈린 곳은 아래 카드에서 직접 확인하세요.')).toBeTruthy();
  expect(screen.getByRole('button', { name: '근거 카드 보기 · 다른 말 1 · 확인 필요 0 · 같은 말 1 · 한 AI만 2' })).toBeTruthy();
  expect(screen.getByText('종합 답변을 쓰는 AI의 회사가 질문과 모든 답(AI 이름 포함)을 받습니다.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '근거 카드 보기 · 다른 말 1 · 확인 필요 0 · 같은 말 1 · 한 AI만 2' }));
  expect(screen.getByRole('button', { name: '근거 카드 접기' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '다른 말 1' })).toBeTruthy();
});

test('in Korean Settings has the combined answer section', async () => {
  setLang('ko');
  await open('/settings');
  expect(await screen.findByRole('switch', { name: '비교가 끝나면 종합 답변 쓰기' })).toBeChecked();
  expect(screen.getByText('쓰는 AI')).toBeTruthy();
  expect(screen.getByText('질문마다 AI 하나를 한 번 더 씁니다(질문 단계의 모델). 그 AI의 회사가 질문과 모든 답을 받습니다.')).toBeTruthy();
  expect(screen.getByRole('radio', { name: 'Claude' })).toBeChecked();
});
