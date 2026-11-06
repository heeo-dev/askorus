// P2-3 S5b / C6: the combined answer on 한 번에, against a pretend agent
// whose moderator writes the answers into one (fakeAgent enableSynth,
// combinedText) and, like the agent since C6, judges no more: its one step
// and grey lines, the answer with the AIs it names as chips, copying it,
// the writer picked in Settings, writing it by hand or again, stopping it,
// Korean, and question refining (off, and turned on). Every user event is
// awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { StyleSheet } from 'react-native';

import { conversations } from '@/ask/conversations';
import { features } from '@/ask/features';
import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, REFINE_TEMPLATE, TOKEN } from '@/test/fakeAgent';
import { aiPalettes } from '@/ui/theme';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
// The pretend moderator names the AIs it read in braces (#6); on screen they are chips set off by thin spaces,
// and a copy has them as plain names.
const named = 'From Claude, Codex, OpenCode.';
const chip = (name: string) => ` ${name} `;
const namedShown = `From ${chip('Claude')}, ${chip('Codex')}, ${chip('OpenCode')}.`;
const mayErr = 'An AI wrote this combined answer, so it can be wrong.';
const step = 'Writing the combined answer…';

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
const skeleton = () => screen.queryByTestId('skeleton', { includeHiddenElements: true });
const close = () => screen.getByRole('button', { name: 'Close' });
// renderRouter runs fake timers: time passes only when advanced.
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

test('while the moderator writes: its one step over grey lines; then the answer, the AIs it names as chips', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'Tell me about Seoul');
  expect(await screen.findByText('Once every answer is in, the moderator AI writes a combined answer.')).toBeTruthy();

  // Being written: the one step is current, with the grey lines of the answer to come.
  expect(await screen.findByLabelText(step, {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByLabelText(/^Next step/)).toBeNull();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(skeleton()).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));
  expect(screen.queryByRole('button', { name: 'Compare again' })).toBeNull();

  agent.synthReply = 'ok';
  expect(await written()).toBeTruthy();
  expect(screen.getByText(namedShown)).toBeTruthy();
  // #6: a named AI is a chip in its own avatar colour; no braces show.
  const chipLook = (name: string) => StyleSheet.flatten(screen.getAllByText(chip(name))[0].props.style);
  expect(chipLook('Claude')).toMatchObject({ fontWeight: '600', backgroundColor: aiPalettes.light.sky.bg, color: aiPalettes.light.sky.fg });
  expect(chipLook('Codex')).toMatchObject({ backgroundColor: aiPalettes.light.lilac.bg, color: aiPalettes.light.lilac.fg });
  expect(chipLook('OpenCode')).toMatchObject({ backgroundColor: aiPalettes.light.teal.bg, color: aiPalettes.light.teal.fg });
  expect(screen.queryByText(/\{\{/)).toBeNull();
  expect(screen.queryByLabelText(step)).toBeNull();
  expect(skeleton()).toBeNull();
  expect(screen.getByText(mayErr)).toBeTruthy();
  expect(screen.getByText('answers 1 s · combined in 15 s')).toBeTruthy();
  expect(agent.runRequests()[0].compare).toEqual({ earlier: [], judges: ['claude', 'codex', 'opencode'], synthesis: { agent: 'claude' } });
  expect(agent.synthCalls).toBe(1);

  // Done: the polling stops.
  const gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);
});

test('Copy copies the combined answer’s Markdown, the AIs as plain names', async () => {
  const user = await open();
  await ask(user, 'q');
  await written();
  await user.press(screen.getByRole('button', { name: 'Copy' }));
  expect(Clipboard.setStringAsync).toHaveBeenLastCalledWith(`Fake combined answer.\n\n${named}`);
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('the writing AI picked in Settings is asked for and writes', async () => {
  const user = await open({ synthAgent: 'codex' });
  await ask(user, 'q');
  await written();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ agent: 'codex' });
  expect(agent.synthCalls).toBe(1);
});

// 11-06: the moderator model set in Settings for the writer goes with the question.
test('the moderator model set for the writing AI goes with the question', async () => {
  const user = await open({ synthAgent: 'codex', synthModels: { codex: { model: 'codex-deep', effort: 'high' }, claude: { model: 'opus' } } });
  await ask(user, 'q');
  await written();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ agent: 'codex', model: 'codex-deep', effort: 'high' });
});

test('with the combined answer off: no request for one, and Write one sends it', async () => {
  const user = await open({ synth: false, synthAgent: 'opencode' });
  // The intro does not promise one.
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  expect(screen.queryByText(/^Once the answers are in, the moderator AI/)).toBeNull();
  await ask(user, 'q');
  expect(await screen.findByRole('button', { name: 'Write a combined answer' }, { timeout: 5000 })).toBeTruthy();
  expect(screen.queryByText(/^Once every answer is in/)).toBeNull();
  expect(agent.runRequests()[0].compare.synthesis).toEqual({ off: true });
  expect(agent.synthCalls).toBe(0);
  expect(screen.getByText("This uses one of the PC's AIs 1 more time.")).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Comparison' })).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'Write a combined answer' }));
  expect(await written()).toBeTruthy();
  expect(agent.synthRequests()).toEqual([{ agent: 'opencode' }]);
  expect(agent.synthCalls).toBe(1);
  expect(screen.queryByRole('button', { name: 'Write a combined answer' })).toBeNull();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
});

test('a writer that fails says why in its words; Write again writes it', async () => {
  agent.synthReply = 'fail';
  const user = await open();
  await ask(user, 'q');
  const why = 'The writing AI reported an error.\nusage limit reached';
  expect(await screen.findByText(why, {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByRole('header', { name: 'Combined answer' })).toBeTruthy();
  expect(screen.queryByText(mayErr)).toBeNull();

  agent.synthReply = 'ok';
  await user.press(screen.getByRole('button', { name: 'Write again' }));
  expect(await written()).toBeTruthy();
  expect(agent.synthRequests()).toEqual([{ agent: 'claude' }]);
  expect(agent.synthCalls).toBe(2);
  expect(screen.queryByText(/^The writing AI reported an error/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Write again' })).toBeNull();
});

test('New question while the combined answer is written stops nothing: it is written on and kept in the history (#23)', async () => {
  agent.synthReply = 'hold';
  const user = await open();
  await ask(user, 'q');
  expect(await screen.findByLabelText(step, {}, { timeout: 5000 })).toBeTruthy();
  await waitFor(() => expect(agent.synthCalls).toBe(1));

  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(screen.queryByLabelText(step)).toBeNull();
  let gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET').length).toBeGreaterThan(gets); // still followed, in the background

  agent.synthReply = 'ok'; // the writing already started finishes
  const id = conversations.list().find((c) => c.title === 'q')?.id ?? '';
  await waitFor(async () => expect((await conversations.load(id))?.turns[0].compare?.snap?.synthesis?.status).toBe('done'), { timeout: 5000 });
  expect(agent.compareCalls('POST', '/cancel')).toHaveLength(0);
  expect(agent.synthCalls).toBe(1);
  // Done: the polling stops.
  gets = agent.compareCalls('GET').length;
  await later();
  expect(agent.compareCalls('GET')).toHaveLength(gets);
});

test('in Korean the combined answer reads in Korean', async () => {
  setLang('ko');
  agent.synthReply = 'hold';
  const user = await open();
  expect(await screen.findByText(t('ask.introModerator'))).toBeTruthy();
  await user.type(await screen.findByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: t('ask.send') }));
  expect(await screen.findByText('다 모이면 사회자 AI가 종합 답변을 씁니다.')).toBeTruthy();
  expect(await screen.findByLabelText('종합 답변 쓰는 중…', {}, { timeout: 5000 })).toBeTruthy();
  agent.synthReply = 'ok';
  await written();
  expect(screen.getByRole('header', { name: '종합 답변' })).toBeTruthy();
  expect(screen.getByText('AI가 쓴 종합이라 틀릴 수 있습니다.')).toBeTruthy();
  expect(screen.getByText('답 1초 · 종합 15초')).toBeTruthy();
  expect(screen.queryByText(/판정|근거/)).toBeNull();
});

// Milestone C: question refining, off since 2026-11-04 (features.ts): no
// run asks for it, whatever the setting says.
test('with the refining feature off, no run asks for refining, even with the setting on', async () => {
  const user = await open({ refine: true });
  await ask(user, 'Tell me about Seoul');
  await written();
  expect(agent.runRequests()[0].refine).toBeUndefined();
  expect(screen.queryByText('Sent without refining')).toBeNull();
  expect(screen.queryByRole('button', { name: /^Answer format/ })).toBeNull();
});

// With the feature turned on (its code stays): the moderator is the writer
// picked in Settings; the run asks for it, the answer format shows under
// the question and opens what was sent with it. The fake agent shows it
// running on the POST's 202 and ended on the first GET.
describe('question refining, turned on', () => {
  beforeEach(() => {
    features.refine = true;
  });
  afterEach(() => {
    features.refine = false;
  });

  test('set on: the run asks the writer to moderate, and the answer format opens what was sent', async () => {
    const user = await open({ refine: true });
    await ask(user, 'Tell me about Seoul');
    expect(agent.runRequests()[0].refine).toEqual({ agent: 'claude' });
    expect(agent.runRequests()[0].prompt).toBe('Tell me about Seoul'); // the phone sends the question as written
    await user.press(await screen.findByRole('button', { name: 'Answer format: Comparison. Show' }, { timeout: 5000 }));
    const sheet = within(await screen.findByTestId('refine-sheet'));
    expect(sheet.getByRole('header', { name: 'Answer format sent' })).toBeTruthy();
    for (const text of ['Comparison', 'Short', 'How the two compare', REFINE_TEMPLATE, 'Your question went as written, with this format added after it, to every AI.', 'Moderator: Claude claude-haiku-5-5 · 4.8 s']) {
      expect(sheet.getByText(text)).toBeTruthy();
    }
    await user.press(close());
    await waitFor(() => expect(screen.queryByTestId('refine-sheet')).toBeNull());
  });

  test('a refining that failed says the question went as written; an open question shows that no format was added', async () => {
    agent.refineReply = 'failed';
    const user = await open({ refine: true });
    await ask(user, 'Tell me about Seoul');
    expect(await screen.findByText('Sent without refining', {}, { timeout: 5000 })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Answer format/ })).toBeNull();

    agent.refineReply = 'open';
    await user.press(screen.getByRole('button', { name: 'New question' }));
    await ask(user, 'Write a poem');
    await user.press(await screen.findByRole('button', { name: 'Answer format: Other. Show' }, { timeout: 5000 }));
    const sheet = within(await screen.findByTestId('refine-sheet'));
    expect(sheet.getByText('No format was added for this kind.')).toBeTruthy();
  });

  test('turned off in Settings, or with another moderator picked, the run asks as set', async () => {
    let user = await open({ refine: false });
    await ask(user, 'Tell me about Seoul');
    await screen.findByText('Fake combined answer.', {}, { timeout: 5000 });
    expect(agent.runRequests()[0].refine).toBeUndefined();
    expect(screen.queryByText('Sent without refining')).toBeNull();
    expect(screen.queryByRole('button', { name: /^Answer format/ })).toBeNull();

    // Another writer picked moderates too.
    user = await open({ synthAgent: 'codex', refine: true });
    await ask(user, 'Tell me about Busan');
    await waitFor(() => expect(agent.runRequests().at(-1)?.refine).toEqual({ agent: 'codex' }));
  });

  test('in Korean', async () => {
    setLang('ko');
    const user = await open({ refine: true });
    await user.type(await screen.findByLabelText(t('ask.placeholder')), '서울은?');
    await user.press(screen.getByRole('button', { name: '묻기' }));
    await user.press(await screen.findByRole('button', { name: '답 틀: 비교. 보기' }, { timeout: 5000 }));
    const sheet = within(await screen.findByTestId('refine-sheet'));
    expect(sheet.getByRole('header', { name: '보낸 답 틀' })).toBeTruthy();
    expect(sheet.getByText('짧게')).toBeTruthy();
    expect(sheet.getByText('사회자: Claude claude-haiku-5-5 · 4.8초')).toBeTruthy();
  });
});
