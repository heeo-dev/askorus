// S5c: the 한 번에 screen ('/', where the app opens) against a pretend agent:
// no PC, an older agent, the empty screen (what happens to a question,
// recent questions, the AI picker and the tier), questions that are not
// compared (a PC whose AIs do not write, a follow-up to one AI), New
// question, the drawer's history and reopening, another PC, and Korean.
// Every user event is awaited (R-07). The shell reads GET /v1/sessions
// now and then, so those calls are never counted exactly.

import { act, userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import { conversations } from '@/ask/conversations';
import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang, t } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { pickPC } from '@/ui/shell/pcs';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function pairedPC(name = 'DESKTOP-TEST', host = HOST) {
  return new ConnectionStore().add({ kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
}

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await pairedPC();
  if (Object.keys(prefs).length) await saveAskPrefs({ ...defaultAskPrefs, ...prefs });
  const r = await renderRouter(appDir, { initialUrl: '/' });
  return { user, r };
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

async function follow(user: User, question: string, field = 'Follow up', send = 'Send') {
  await user.type(screen.getByLabelText(field), question);
  await user.press(screen.getByRole('button', { name: send }));
}

const close = () => screen.getByRole('button', { name: 'Close' });

test('with no PC it says the PC answers and offers pairing', async () => {
  const user = userEvent.setup();
  const r = await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText("Your PC's AIs answer the questions.")).toBeTruthy();
  expect(screen.getByText(/^Pair a PC first/)).toBeTruthy();
  expect(screen.getByRole('tab', { name: 'All at once' })).toBeSelected();
  await user.press(screen.getByRole('button', { name: 'Pair with a PC' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pair'));
});

test('an agent without asks says to update it; one with no AI that can answer says so', async () => {
  agent = new FakeAgent(); // no ask capability
  globalThis.fetch = agent.fetch;
  const { r } = await open();
  expect(await screen.findByText(/does not know asks yet/)).toBeTruthy();
  expect(screen.queryByLabelText('Ask a question')).toBeNull();
  await r.unmount();

  agent = new FakeAgent();
  agent.enableAsk();
  agent.capabilities = agent.capabilities.filter((c) => c !== 'ask');
  agent.askAgents = agent.askAgents.map((a) => ({ ...a, available: false }));
  globalThis.fetch = agent.fetch;
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText(/^No AI on this PC can answer right now/)).toBeTruthy();
  expect(screen.queryByLabelText('Ask a question')).toBeNull();
});

test('the empty screen asks what to ask and says where a question goes', async () => {
  await open();
  expect(await screen.findByRole('header', { name: 'What would you like to ask?' })).toBeTruthy();
  // This PC's AIs do not write: the plain intro, nothing about companies.
  expect(screen.getByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  expect(screen.queryByText(/company receives/)).toBeNull();
  expect(screen.queryByText('Recent questions')).toBeNull();
  expect(screen.getByRole('button', { name: 'AIs to answer: 3 (Claude, Codex, OpenCode). Pick' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Tier: Standard. Pick' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Ask' })).toBeDisabled();
  // No question yet: no New question button.
  expect(screen.queryByRole('button', { name: 'New question' })).toBeNull();
});

// Milestone C: refining needs an AI that writes; this PC's do not, so no run asks for it.
test('on a PC whose AIs do not write, no question is refined', async () => {
  const { user } = await open();
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(agent.runRequests()[0].refine).toBeUndefined();
  expect(screen.queryByText('Sent without refining')).toBeNull();
});

test('on a PC whose AIs write, the intro says the moderator writes and its company gets the answers (R-34)', async () => {
  agent.enableSynth();
  await open();
  expect(await screen.findByText('One question goes to every AI turned on, at once.')).toBeTruthy();
  expect(
    screen.getByText('Once the answers are in, the moderator AI reads them all and writes a combined answer. Its company receives the question and every answer.'),
  ).toBeTruthy();
  expect(screen.queryByText(/judg/)).toBeNull();
});

test('the AI picker turns AIs on and off and picks a model; they and the tier go with the question and stay', async () => {
  const { user, r } = await open();
  await user.press(await screen.findByRole('button', { name: 'AIs to answer: 3 (Claude, Codex, OpenCode). Pick' }));
  expect(await screen.findByRole('header', { name: 'AIs to answer' })).toBeTruthy();
  expect(screen.getByText('My PC: DESKTOP-TEST')).toBeTruthy();
  expect(screen.getByText('sonnet')).toBeTruthy(); // Claude's model for the tier
  // Each row is the AI's switch, with its state (UX-9); the small switch in
  // it is not a second control with the same name, the › is its own button.
  expect(screen.getAllByRole('switch').map((x) => x.props.accessibilityLabel)).toEqual(['Claude, sonnet', 'Codex, codex-std · medium', 'OpenCode, opencode-std · medium']);
  expect(screen.queryByRole('button', { name: 'OpenCode, opencode-std · medium' })).toBeNull();
  await user.press(screen.getByRole('switch', { name: 'Codex, codex-std · medium' }));
  expect(screen.getByRole('switch', { name: 'Codex, codex-std · medium' })).not.toBeChecked();
  // The small switch turns it too (hidden from screen readers: the row says it).
  const small = (agent: string) => screen.getByTestId(`switch-${agent}`, { includeHiddenElements: true });
  await user.press(small('opencode'));
  expect(screen.getByRole('switch', { name: 'OpenCode, opencode-std · medium' })).not.toBeChecked();
  // The last AI that is on stays on (UX-10): a question always has an AI to go to.
  await user.press(screen.getByRole('switch', { name: 'Claude, sonnet' }));
  expect(screen.getByRole('switch', { name: 'Claude, sonnet' })).toBeChecked();
  await user.press(small('claude'));
  expect(screen.getByRole('switch', { name: 'Claude, sonnet' })).toBeChecked();
  await user.press(screen.getByRole('switch', { name: 'OpenCode, opencode-std · medium' }));
  expect(screen.getByRole('switch', { name: 'OpenCode, opencode-std · medium' })).toBeChecked();

  // A model for Claude: the model sheet, then back to the picker.
  await user.press(screen.getByRole('button', { name: 'Claude model' }));
  expect(await screen.findByRole('header', { name: 'Claude model' })).toBeTruthy();
  expect(screen.getByRole('radio', { name: 'By tier' })).toBeChecked();
  expect(screen.getByText('Quick haiku\nStandard sonnet\nDeep opus')).toBeTruthy();
  // The row (the own pick's model pill has the same name).
  await user.press(screen.getAllByRole('radio', { name: 'opus' })[0]);
  expect(await screen.findByRole('header', { name: 'AIs to answer' })).toBeTruthy();
  expect(screen.getByRole('switch', { name: 'Claude, opus' })).toBeChecked();
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('ai-sheet')).toBeNull());

  expect(screen.getByRole('button', { name: 'AIs to answer: 2 (Claude, OpenCode). Pick' })).toBeTruthy();
  // The tier chip opens the tier sheet (#16): each tier with the models of
  // the AIs that are on and follow it (Claude has its own model now).
  await user.press(screen.getByRole('button', { name: 'Tier: Standard. Pick' }));
  expect(await screen.findByRole('header', { name: 'Tier' })).toBeTruthy();
  expect(screen.getByRole('radio', { name: 'Standard' })).toBeChecked();
  expect(screen.getByText('OpenCode opencode-deep · medium')).toBeTruthy();
  expect(screen.getByText('OpenCode opencode-quick · medium')).toBeTruthy();
  await user.press(screen.getByRole('radio', { name: 'Deep' }));
  await waitFor(() => expect(screen.queryByTestId('tier-sheet')).toBeNull());
  expect(screen.getByRole('button', { name: 'Tier: Deep. Pick' })).toBeTruthy();
  await ask(user, 'q1');
  expect(await screen.findByText('opencode says: q1')).toBeTruthy();
  expect(agent.runRequests()[0]).toMatchObject({ tier: 'deep', agents: [{ agent: 'claude', model: 'opus' }, { agent: 'opencode' }] });
  expect(await loadAskPrefs()).toMatchObject({ tier: 'deep', off: ['codex'], models: { claude: 'opus' } });

  // Kept for next time; a long press on a row opens its model too.
  await r.unmount();
  const user2 = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByRole('button', { name: 'Tier: Deep. Pick' })).toBeTruthy();
  await user2.press(screen.getByRole('button', { name: 'AIs to answer: 2 (Claude, OpenCode). Pick' }));
  expect(await screen.findByRole('switch', { name: 'Codex, codex-deep · medium' })).not.toBeChecked();
  await user2.longPress(screen.getByRole('switch', { name: 'Claude, opus' }));
  expect((await screen.findAllByRole('radio', { name: 'opus' }))[0]).toBeChecked();
  await user2.press(screen.getByRole('radio', { name: 'By tier' }));
  expect(await screen.findByRole('switch', { name: 'Claude, opus' })).toBeTruthy(); // Deep by tier is opus too
  await waitFor(async () => expect((await loadAskPrefs()).models).toEqual({}));
});

// R-74 (S23, 11-06): OpenCode's Flash at quick and at standard read as one
// model, so its effort could not be picked; now every row says its effort,
// and an own pick takes any of the tiers' models with an effort the PC lists.
test("the model sheet lists each tier's model with its effort, and an own model and effort go with the question", async () => {
  const flash = 'opencode-go/deepseek-v4.1-flash';
  const pro = 'opencode-go/deepseek-v4-pro';
  agent.askModels(
    'opencode',
    { quick: { model: flash, effort: 'low' }, standard: { model: flash, effort: 'high' }, deep: { model: pro, effort: 'high' } },
    { [flash]: ['low', 'high', 'max'], [pro]: ['high', 'max'] },
  );
  const { user } = await open();
  await user.press(await screen.findByRole('button', { name: 'AIs to answer: 3 (Claude, Codex, OpenCode). Pick' }));
  expect(await screen.findByRole('switch', { name: 'OpenCode, deepseek-v4.1-flash · high' })).toBeChecked(); // standard
  await user.press(screen.getByRole('button', { name: 'OpenCode model' }));
  expect(await screen.findByRole('header', { name: 'OpenCode model' })).toBeTruthy();
  expect(screen.getByText('Quick deepseek-v4.1-flash · low\nStandard deepseek-v4.1-flash · high\nDeep deepseek-v4-pro · high')).toBeTruthy();
  expect(screen.getAllByRole('radio').map((x) => x.props.accessibilityLabel).slice(0, 4)).toEqual([
    'By tier',
    'deepseek-v4.1-flash, low',
    'deepseek-v4.1-flash, high',
    'deepseek-v4-pro, high',
  ]);
  // A pair of the tiers: back to the picker.
  await user.press(screen.getByRole('radio', { name: 'deepseek-v4.1-flash, low' }));
  expect(await screen.findByRole('switch', { name: 'OpenCode, deepseek-v4.1-flash · low' })).toBeTruthy();

  // An own pick: Pro has no low, so it takes Pro's tier effort; then max. The sheet stays.
  await user.press(screen.getByRole('button', { name: 'OpenCode model' }));
  const models = await screen.findByLabelText('Pick your own: Model');
  expect(within(models).getByRole('radio', { name: 'deepseek-v4.1-flash' })).toBeChecked();
  expect(within(screen.getByLabelText('Pick your own: Effort')).getAllByRole('radio').map((x) => x.props.accessibilityLabel)).toEqual(['low', 'high', 'max']);
  await user.press(within(models).getByRole('radio', { name: 'deepseek-v4-pro' }));
  expect(await screen.findByRole('radio', { name: 'deepseek-v4-pro, high' })).toBeChecked();
  const efforts = screen.getByLabelText('Pick your own: Effort');
  expect(within(efforts).getAllByRole('radio').map((x) => x.props.accessibilityLabel)).toEqual(['high', 'max']);
  await user.press(within(efforts).getByRole('radio', { name: 'max' }));
  expect(await screen.findByRole('radio', { name: 'max' })).toBeChecked();
  expect(screen.getByRole('radio', { name: 'deepseek-v4-pro, high' })).not.toBeChecked();
  await user.press(close());
  expect(await screen.findByRole('switch', { name: 'OpenCode, deepseek-v4-pro · max' })).toBeTruthy();
  await user.press(close());
  await waitFor(() => expect(screen.queryByTestId('ai-sheet')).toBeNull());

  await ask(user, 'q1');
  expect(await screen.findByText('opencode says: q1')).toBeTruthy();
  expect(agent.runRequests()[0].agents[2]).toEqual({ agent: 'opencode', model: pro, effort: 'max' });
  expect(await loadAskPrefs()).toMatchObject({ models: { opencode: pro }, efforts: { opencode: 'max' } });
});

test('with every AI turned off (kept from before), the picker says so and turning one on is enough (UX-10)', async () => {
  const { user } = await open({ off: ['claude', 'codex', 'opencode'] });
  await user.type(await screen.findByLabelText('Ask a question'), 'q');
  expect(screen.getByRole('button', { name: 'Ask' })).toBeDisabled();
  await user.press(screen.getByRole('button', { name: 'No AI is on to answer. Pick' }));
  await user.press(await screen.findByRole('switch', { name: 'Codex, codex-std · medium' }));
  expect(screen.getByRole('switch', { name: 'Codex, codex-std · medium' })).toBeChecked();
  await user.press(close());
  expect(await screen.findByRole('button', { name: 'AIs to answer: 1 (Codex). Pick' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Ask' })).toBeEnabled();
});

test('an AI the PC cannot use is dimmed in the picker and never asked', async () => {
  agent.askAgents[1] = { agent: 'opencode', available: false, capabilities: [], notices: ['opencode_not_found'] };
  const { user } = await open();
  await user.press(await screen.findByRole('button', { name: 'AIs to answer: 2 (Claude, Codex). Pick' }));
  expect(await screen.findByRole('switch', { name: 'OpenCode, This AI cannot answer on this PC right now.' })).toBeDisabled();
  expect(screen.getByText('This AI cannot answer on this PC right now.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'OpenCode model' })).toBeNull();
  await user.press(close());
  await ask(user, 'q');
  await screen.findByText('codex says: q');
  expect(agent.runRequests()[0].agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }]);
});

test('a question no AI compares shows each answer in full as it comes, with its model and time', async () => {
  agent.runLimit = 1;
  const { user } = await open();
  await ask(user, '대한민국의 수도는?');
  expect(await screen.findByText('claude says: 대한민국의 수도는?')).toBeTruthy();
  expect(screen.getByText('대한민국의 수도는?')).toBeTruthy(); // the question bubble
  await waitFor(() => expect(screen.getAllByText('Answering…')).toHaveLength(2));
  expect(screen.getByText('codex-standard · Answering…')).toBeTruthy();
  // The follow-up waits for every answer, and says so.
  const send = screen.getByRole('button', { name: 'Send' });
  expect(send).toBeDisabled();
  expect(send.props.accessibilityHint).toBe('You can send once every answer is in.');

  agent.runLimit = Infinity;
  expect(await screen.findByText('opencode says: 대한민국의 수도는?')).toBeTruthy();
  expect(screen.queryByText('Answering…')).toBeNull();
  expect(screen.getAllByText(/^\w+-standard · 1\.2 s$/)).toHaveLength(3); // model, then time
  expect(screen.getByLabelText("Codex's answer")).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Send' }).props.accessibilityHint).toBeUndefined();
  const [req] = agent.runRequests();
  expect(req).toMatchObject({ mode: 'ask', prompt: '대한민국의 수도는?', tier: 'standard' });
  expect(req.agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }, { agent: 'opencode' }]);
  expect(req.compare).toBeUndefined();
  expect(req.clientRunId).toMatch(/^[0-9a-f-]{36}$/);
});

test('follow-ups carry each AI on its own conversation; Only this AI asks just that one, with its chip', async () => {
  const { user } = await open();
  await ask(user, 'first');
  await screen.findByText('opencode says: first');

  await user.press(screen.getAllByRole('button', { name: 'Only this AI' })[1]);
  expect(screen.getByText('Only Codex')).toBeTruthy();
  expect(screen.getByLabelText('Ask Codex a follow-up')).toBeTruthy();
  await follow(user, 'only you', 'Ask Codex a follow-up', 'Send to Codex');
  await screen.findByText('codex says: only you');
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);
  // After sending, the next one starts with the last question's AIs (#22, 11-07): Codex
  // again, until its chip is cancelled (everyone turned on).
  expect(screen.getByText('Only Codex')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Stop asking only Codex' }));
  expect(screen.queryByText('Only Codex')).toBeNull();

  // The chip can be cancelled before sending.
  await user.press(screen.getAllByRole('button', { name: 'Only this AI' })[0]);
  expect(screen.getByText('Only Claude')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Stop asking only Claude' }));
  expect(screen.queryByText('Only Claude')).toBeNull();
  expect(screen.getByLabelText('Follow up')).toBeTruthy();

  // Back to everyone: each AI goes on with its latest conversation.
  await follow(user, 'all again');
  await screen.findByText('opencode says: all again');
  expect(agent.runRequests()[2].agents).toEqual([
    { agent: 'claude', session: 'claude:s-1' },
    { agent: 'codex', session: 'codex:s-1' },
    { agent: 'opencode', session: 'opencode:s-1' },
  ]);

  // A new question starts new conversations.
  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(screen.queryByText('opencode says: first')).toBeNull();
  await ask(user, 'fresh');
  await screen.findByText('opencode says: fresh');
  expect(agent.runRequests()[3].agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }, { agent: 'opencode' }]);
});

// #22 (11-07): the follow-up bar has the AIs and the tier, for this conversation:
// by default the last question's, picked in their own sheets, one AI as its chip.
test("the follow-up bar picks the AIs and the tier of the next follow-up, starting with the last question's", async () => {
  const { user } = await open();
  await ask(user, 'first');
  await screen.findByText('opencode says: first');
  const pile = (n: number, names: string) => screen.getByRole('button', { name: `AIs for the follow-up: ${n} (${names}). Pick` });
  expect(pile(3, 'Claude, Codex, OpenCode')).toBeTruthy();
  expect(within(screen.getByTestId('follow-bar')).getByRole('button', { name: 'Tier: Standard. Pick' })).toBeTruthy();

  // Two of the three, on the quick tier.
  await user.press(pile(3, 'Claude, Codex, OpenCode'));
  expect(await screen.findByRole('header', { name: 'AIs for the follow-up' })).toBeTruthy();
  await user.press(screen.getByRole('switch', { name: 'OpenCode, opencode-std · medium' }));
  expect(screen.getByRole('switch', { name: 'OpenCode, opencode-std · medium' })).not.toBeChecked();
  await user.press(screen.getByLabelText('Close'));
  expect(pile(2, 'Claude, Codex')).toBeTruthy();
  await user.press(within(screen.getByTestId('follow-bar')).getByRole('button', { name: 'Tier: Standard. Pick' }));
  await user.press(await screen.findByRole('radio', { name: 'Quick' }));
  await follow(user, 'two of you');
  await screen.findByText('codex says: two of you');
  expect(agent.runRequests()[1]).toMatchObject({
    tier: 'quick',
    agents: [
      { agent: 'claude', session: 'claude:s-1' },
      { agent: 'codex', session: 'codex:s-1' },
    ],
  });
  // The follow-up's picks are this conversation's: the AIs turned on and the tier set stay.
  expect((await loadAskPrefs()).off).toEqual([]);
  expect((await loadAskPrefs()).tier).toBe('standard');
  // The next one starts with the last question's AIs and tier.
  expect(pile(2, 'Claude, Codex')).toBeTruthy();
  expect(within(screen.getByTestId('follow-bar')).getByRole('button', { name: 'Tier: Quick. Pick' })).toBeTruthy();

  // Down to one AI: its chip (the old Only this AI); the last one stays on.
  await user.press(pile(2, 'Claude, Codex'));
  await screen.findByRole('header', { name: 'AIs for the follow-up' });
  // The models are the follow-up tier's.
  expect(screen.getAllByRole('switch').map((x) => x.props.accessibilityLabel)).toEqual(['Claude, haiku', 'Codex, codex-quick · medium', 'OpenCode, opencode-quick · medium']);
  await user.press(screen.getByRole('switch', { name: 'Codex, codex-quick · medium' }));
  await user.press(screen.getByRole('switch', { name: 'Claude, haiku' }));
  expect(screen.getByRole('switch', { name: 'Claude, haiku' })).toBeChecked();
  await user.press(screen.getByLabelText('Close'));
  expect(within(screen.getByTestId('follow-bar')).getByText('Only Claude')).toBeTruthy();
  expect(screen.getByLabelText('Ask Claude a follow-up')).toBeTruthy();
  // Cancelled: everyone turned on.
  await user.press(screen.getByRole('button', { name: 'Stop asking only Claude' }));
  expect(pile(3, 'Claude, Codex, OpenCode')).toBeTruthy();

  // A new question starts from the AIs turned on and the tier set.
  await user.press(screen.getByRole('button', { name: 'New question' }));
  await ask(user, 'second');
  await screen.findByText('opencode says: second');
  expect(agent.runRequests()[2]).toMatchObject({ prompt: 'second', tier: 'standard' });
  expect(agent.runRequests()[2].agents).toHaveLength(3);
});

test('answers are drawn from their Markdown; copy keeps the Markdown', async () => {
  agent.askReplies.claude = '수도는 **서울**입니다.\n\n- 인구: 약 930만';
  const { user } = await open();
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(screen.queryByText(/\*\*/)).toBeNull();
  expect(screen.getByText('서울')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText('인구: 약 930만')).toBeTruthy();
  await user.press(screen.getAllByRole('button', { name: 'Copy' })[0]);
  expect(Clipboard.setStringAsync).toHaveBeenCalledWith('수도는 **서울**입니다.\n\n- 인구: 약 930만');
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('an AI that fails says why while the others answer', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.askReplies.opencode = { code: 'ask_timeout' };
  const { user } = await open();
  await ask(user, 'q');
  expect(await screen.findByText('claude says: q')).toBeTruthy();
  expect(await screen.findByText('The AI reported an error.\nusage limit reached')).toBeTruthy();
  expect(screen.getByText('No answer within 3 minutes.')).toBeTruthy();
  expect(screen.getAllByRole('button', { name: 'Copy' })).toHaveLength(1); // only an answer can be copied
});

test('a refused run says why, and trying again sends the same run', async () => {
  agent.runError = { status: 429, code: 'too_many_runs' };
  const { user } = await open();
  await ask(user, 'q');
  expect(await screen.findByText(/The PC is answering other questions/)).toBeTruthy();
  expect(screen.queryByText('Did not answer')).toBeNull(); // the turn says why once
  agent.runError = null;
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('claude says: q')).toBeTruthy();
  const [a, b] = agent.runRequests();
  expect(b.clientRunId).toBe(a.clientRunId);
  expect(screen.queryByText(/The PC is answering other questions/)).toBeNull();
});

test("Claude's answer continues in the Claude app; any answer copies", async () => {
  const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const { user } = await open();
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(screen.getAllByRole('button', { name: 'Continue in the Claude app' })).toHaveLength(1); // Claude's only

  await user.press(screen.getByRole('button', { name: 'Continue in the Claude app' }));
  await waitFor(() => expect(openURL).toHaveBeenCalledWith(agent.remoteUrl));
  expect(agent.callsTo('POST', `/v1/sessions/${encodeURIComponent('claude:s-1')}/open`)).toHaveLength(1);

  await user.press(screen.getAllByRole('button', { name: 'Copy' })[1]);
  expect(Clipboard.setStringAsync).toHaveBeenCalledWith('codex says: q');
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('the ask folder that needs trusting is explained when continuing in the Claude app', async () => {
  const { user } = await open();
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  agent.openError = { code: 'workspace_not_trusted', message: 'C:\\Users\\me\\AppData\\Local\\Asksemble\\ask' };
  await user.press(screen.getByRole('button', { name: 'Continue in the Claude app' }));
  expect(await screen.findByText(/trust the ask folder on the PC once: in a PC terminal, go to C:\\Users\\me\\AppData\\Local\\Asksemble\\ask/)).toBeTruthy();
});

test('New question keeps the conversation: recent questions and the drawer bring it back', async () => {
  const { user } = await open();
  await ask(user, 'How tall is Everest?');
  await screen.findByText('opencode says: How tall is Everest?');
  expect(conversations.current()?.title).toBe('How tall is Everest?');

  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(await screen.findByText('Recent questions')).toBeTruthy();
  expect(conversations.current()).toBeNull();
  expect(conversations.list().map((c) => c.title)).toEqual(['How tall is Everest?']);
  expect((await conversations.load(conversations.list()[0].id))?.turns).toHaveLength(1);
  // A recent question fills the box.
  await user.press(screen.getByRole('button', { name: 'How tall is Everest?' }));
  expect(screen.getByLabelText('Ask a question').props.value).toBe('How tall is Everest?');
  await user.clear(screen.getByLabelText('Ask a question'));
  await ask(user, 'Coffee per day?');
  await screen.findByText('opencode says: Coffee per day?');

  // The drawer: the current one first, the earlier one under today; opening it keeps this one.
  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  expect(await screen.findByRole('button', { name: 'Coffee per day?' })).toBeSelected();
  await user.press(screen.getByRole('button', { name: 'How tall is Everest?' }));
  expect(await screen.findByText('opencode says: How tall is Everest?')).toBeTruthy();
  expect(screen.queryByText('opencode says: Coffee per day?')).toBeNull();
  expect(conversations.current()?.title).toBe('How tall is Everest?');
  expect(conversations.list().map((c) => c.title)).toEqual(['Coffee per day?', 'How tall is Everest?']);

  // It carries on: the follow-up goes on with that conversation's sessions.
  await follow(user, 'And K2?');
  await screen.findByText('opencode says: And K2?');
  expect(agent.runRequests()[2].agents).toEqual([
    { agent: 'claude', session: 'claude:s-1' },
    { agent: 'codex', session: 'codex:s-1' },
    { agent: 'opencode', session: 'opencode:s-1' },
  ]);
});

test('milestone B: the history is kept on the phone; after the app starts again the drawer opens a conversation, answers and all, and it carries on', async () => {
  const { user, r } = await open();
  await ask(user, 'How tall is Everest?');
  await screen.findByText('opencode says: How tall is Everest?');
  // No 'New question': the conversation on screen is kept as it is (about once a second).
  expect(conversations.list()).toEqual([]);
  await act(() => jest.advanceTimersByTimeAsync(1500));
  expect(conversations.list().map((c) => c.title)).toEqual(['How tall is Everest?']);

  // The app is closed and started again: the memory is gone, the phone's store is not.
  await r.unmount();
  conversations.reset();
  const user2 = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText('Recent questions')).toBeTruthy();
  await user2.press(screen.getByRole('button', { name: 'Open menu' }));
  await user2.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'How tall is Everest?' }));
  expect(await screen.findByText('opencode says: How tall is Everest?')).toBeTruthy();
  expect(conversations.current()?.title).toBe('How tall is Everest?');

  await follow(user2, 'And K2?');
  await screen.findByText('opencode says: And K2?');
  expect(agent.runRequests().at(-1)?.agents).toEqual([
    { agent: 'claude', session: 'claude:s-1' },
    { agent: 'codex', session: 'codex:s-1' },
    { agent: 'opencode', session: 'opencode:s-1' },
  ]);
});

test('a conversation left while its answers were coming is followed again when reopened', async () => {
  agent.runLimit = 1;
  const { user } = await open();
  await ask(user, 'slow one');
  await screen.findByText('claude says: slow one');
  await user.press(screen.getByRole('button', { name: 'New question' }));
  const posts = agent.runRequests().length;
  agent.runLimit = Infinity;

  await user.press(screen.getByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'slow one' }));
  expect(await screen.findByText('opencode says: slow one', {}, { timeout: 5000 })).toBeTruthy();
  expect(agent.runRequests()).toHaveLength(posts); // followed by its run id, not asked again
  expect(screen.queryByText('Answering…')).toBeNull();
});

test('the questions go to the current PC; a conversation stays with the PC it was asked on', async () => {
  const laptop = new FakeAgent();
  laptop.enableAsk();
  const laptopHost = 'laptop.tail0.ts.net';
  globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(u));
    if (url.hostname === laptopHost) return laptop.fetch(`https://${HOST}:${PORT}${url.pathname}${url.search}`, init);
    return agent.fetch(u, init);
  }) as typeof fetch;
  const user = userEvent.setup();
  await pairedPC();
  const lap = await pairedPC('LAPTOP', laptopHost);
  await renderRouter(appDir, { initialUrl: '/' });
  await ask(user, 'on the desktop');
  await screen.findByText('opencode says: on the desktop');
  expect(agent.runRequests()).toHaveLength(1);

  // 내 PC picks the laptop: this conversation is kept, a new one starts there.
  await act(() => pickPC(lap.id));
  expect(await screen.findByRole('header', { name: 'What would you like to ask?' }, { timeout: 5000 })).toBeTruthy();
  expect(conversations.list().map((c) => c.title)).toEqual(['on the desktop']);
  await user.press(await screen.findByRole('button', { name: /^AIs to answer: 3/ }));
  expect(await screen.findByText('My PC: LAPTOP')).toBeTruthy();
  await user.press(close());
  await ask(user, 'on the laptop');
  await screen.findByText('opencode says: on the laptop');
  expect(laptop.runRequests()).toHaveLength(1);
  expect(agent.runRequests()).toHaveLength(1);
});

test('in Korean the screen reads in Korean', async () => {
  setLang('ko');
  const { user } = await open();
  expect(await screen.findByRole('header', { name: '무엇을 물어볼까요?' })).toBeTruthy();
  expect(screen.getByText('질문 하나를 켜 둔 AI 모두에게 한 번에 묻습니다.')).toBeTruthy();
  expect(screen.getByRole('button', { name: '답할 AI 3개: Claude, Codex, OpenCode. 고르기' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '단계: 기본. 고르기' })).toBeTruthy();
  await user.type(screen.getByLabelText(t('ask.placeholder')), 'q');
  await user.press(screen.getByRole('button', { name: '묻기' }));
  await screen.findByText('opencode says: q');
  expect(screen.getAllByText(/^\w+-standard · 1\.2초$/)).toHaveLength(3);
  expect(screen.getAllByRole('button', { name: '이 AI에게만' })).toHaveLength(3);
  expect(screen.getByLabelText('이어서 묻기')).toBeTruthy();
  await user.press(screen.getAllByRole('button', { name: '이 AI에게만' })[1]);
  expect(screen.getByText('Codex에게만')).toBeTruthy();
  expect(screen.getByLabelText('Codex에게 이어서 묻기')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Codex에게 보내기' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Codex에게만 묻기 취소' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '새 질문' })).toBeTruthy();
});
