// #29 대화 결론 기록: a follow-up carries what was concluded so far, against
// a pretend agent that takes a record (fakeAgent enableContext) and runs
// debates. The notice the first time and '이번만 빼고 보내기', the line
// under the question and its sheet, the Settings switch, an older agent,
// a follow-up sent while the debate runs (it waits, then goes with the
// debate's outcome), and the follow-up's own debate with the record. Every
// user event is awaited (R-07).

import { act, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { DEBATE_AFTER, FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'The first ferry leaves at 7 a.m.',
  codex: 'The first ferry leaves at 8 a.m.',
  opencode: 'It leaves at 8 a.m. from pier 2.',
};
const settled = { topic: 'First ferry time', result: 'agreed', line: 'The first ferry is at 8 a.m. ({{Claude}} changed).' };
// The fake moderator's answer after the debate starts with DEBATE_AFTER and names the AIs that spoke.
const afterText = expect.stringMatching(/^Fake answer after the debate\./);

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.enableDebate();
  agent.enableContext();
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
  await saveAskPrefs({ ...defaultAskPrefs, debateNoticed: true, ...prefs });
  await renderRouter(appDir, { initialUrl: '/' });
  return user;
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

async function follow(user: User, question: string) {
  await user.type(screen.getByLabelText('Follow up'), question);
  await user.press(screen.getByRole('button', { name: 'Send' }));
}

const combined = (n: number) => waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(n), { timeout: 5000 });
const after = () => screen.findByText(DEBATE_AFTER, {}, { timeout: 8000 });
const later = () => act(() => jest.advanceTimersByTimeAsync(200));

/** A first question, its combined answer and its debate to the end. */
async function debated(user: User) {
  await ask(user, 'When is the first ferry?');
  await combined(1);
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  await after();
}

test('after a debate, a follow-up carries the record: the notice once, the line and its sheet', async () => {
  const user = await open();
  await debated(user);

  // The first follow-up that carries a record says where it goes, once.
  await follow(user, 'And the last ferry?');
  expect(screen.getByRole('header', { name: 'Earlier conclusions go along' })).toBeTruthy();
  expect(screen.getByText(/to every AI that answers\. Each AI's company receives it\./)).toBeTruthy();
  expect(agent.runRequests()).toHaveLength(1);
  await user.press(screen.getByRole('button', { name: 'Continue' }));
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).toMatchObject({
    prompt: 'And the last ferry?',
    context: { earlier: ['When is the first ferry?'], settled: [settled], last: afterText },
  });
  expect(screen.queryByRole('header', { name: 'Earlier conclusions go along' })).toBeNull();
  expect((await loadAskPrefs()).recordNoticed).toBe(true);

  // The line under the question opens what was sent.
  await combined(2);
  await user.press(screen.getByRole('button', { name: 'Builds on earlier conclusions. Show' }));
  const sheet = within(screen.getByTestId('record-sheet'));
  expect(sheet.getByText('Earlier conclusions sent')).toBeTruthy();
  expect(sheet.getByText('First ferry time')).toBeTruthy();
  expect(sheet.getByText('Agreed')).toBeTruthy();
  expect(sheet.getByText('The first ferry is at 8 a.m. (Claude changed).')).toBeTruthy();
  expect(sheet.getByText('The last final answer went along too.')).toBeTruthy();
  await user.press(screen.getByLabelText('Close'));

  // The follow-up's own debate carries the record it went with.
  await user.press(screen.getAllByRole('button', { name: 'Debate' }).at(-1)!);
  await waitFor(() => expect(agent.debateRequests()).toHaveLength(2));
  expect(agent.debateRequests()[0]).not.toHaveProperty('context');
  expect(agent.debateRequests()[1].context).toEqual(agent.runRequests()[1].context);

  // No notice again; the next follow-up has the second debate's outcome in.
  await waitFor(() => expect(screen.getAllByText(DEBATE_AFTER)).toHaveLength(2), { timeout: 8000 });
  await follow(user, 'And on Sundays?');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].context).toEqual({
    earlier: ['When is the first ferry?', 'And the last ferry?'],
    settled: [settled, settled],
    last: afterText,
  });
});

test('"Send without them this time" sends the question alone, and the notice is not shown again', async () => {
  const user = await open();
  await debated(user);
  await follow(user, 'And the last ferry?');
  await user.press(screen.getByRole('button', { name: 'Send without them this time' }));
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).not.toHaveProperty('context');
  expect((await loadAskPrefs()).recordNoticed).toBe(true);
  await combined(2);
  expect(screen.queryByRole('button', { name: 'Builds on earlier conclusions. Show' })).toBeNull();
  // The next one carries it, with no notice.
  await follow(user, 'And on Sundays?');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].context).toMatchObject({ last: 'Fake combined answer.\n\nFrom {{Claude}}, {{Codex}}, {{OpenCode}}.' });
  expect(screen.queryByRole('header', { name: 'Earlier conclusions go along' })).toBeNull();
});

test('turned off in Settings, or an older agent: no notice and no record', async () => {
  let user = await open({ carryRecord: false });
  await debated(user);
  await follow(user, 'And the last ferry?');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).not.toHaveProperty('context');
  expect(screen.queryByRole('header', { name: 'Earlier conclusions go along' })).toBeNull();
  expect((await loadAskPrefs()).recordNoticed).toBe(false);
  screen.unmount();

  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.enableDebate();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
  user = await open();
  await debated(user);
  await follow(user, 'And the last ferry?');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).not.toHaveProperty('context');
  expect(screen.queryByRole('header', { name: 'Earlier conclusions go along' })).toBeNull();
});

test('a follow-up sent while the debate runs waits, then goes with its outcome', async () => {
  const user = await open({ recordNoticed: true });
  await ask(user, 'When is the first ferry?');
  await combined(1);
  agent.debateReply = 'hold'; // the debate stays at its first turn until released
  await user.press(screen.getByRole('button', { name: 'Debate' }));
  expect(await screen.findByText('Moderator → Claude: Question to Claude?', {}, { timeout: 5000 })).toBeTruthy();
  await follow(user, 'And the last ferry?');
  expect(screen.getByText("Waiting: it goes with the debate's conclusions once the debate ends.")).toBeTruthy();
  await later();
  await later();
  expect(agent.runRequests()).toHaveLength(1);
  agent.releaseDebates();
  await after();
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2), { timeout: 5000 });
  expect(agent.runRequests()[1]).toMatchObject({
    prompt: 'And the last ferry?',
    context: { earlier: ['When is the first ferry?'], settled: [settled], last: afterText },
  });
  expect(screen.queryByText("Waiting: it goes with the debate's conclusions once the debate ends.")).toBeNull();
  await combined(2);
});

test('a follow-up sent while the combined answer is written waits, then goes with it (11-07)', async () => {
  agent.synthReply = 'hold'; // the combined answer stays "writing" until released
  const user = await open({ recordNoticed: true });
  await ask(user, 'When is the first ferry?');
  expect(await screen.findByText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  await follow(user, 'And the last ferry?');
  expect(screen.getByText('Waiting: it goes with the combined answer once it is written.')).toBeTruthy();
  await later();
  await later();
  expect(agent.runRequests()).toHaveLength(1);
  agent.synthReply = 'ok';
  await combined(1);
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2), { timeout: 5000 });
  expect(agent.runRequests()[1]).toMatchObject({
    prompt: 'And the last ferry?',
    context: { earlier: ['When is the first ferry?'], last: 'Fake combined answer.\n\nFrom {{Claude}}, {{Codex}}, {{OpenCode}}.' },
  });
  expect(agent.runRequests()[1].context).not.toHaveProperty('settled');
  expect(screen.queryByText('Waiting: it goes with the combined answer once it is written.')).toBeNull();
});

test('a follow-up that carries no record does not wait for the combined answer', async () => {
  agent.synthReply = 'hold';
  // Not seen yet: the notice comes first; "without them this time" goes at once.
  const user = await open();
  await ask(user, 'When is the first ferry?');
  expect(await screen.findByText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  await follow(user, 'And the last ferry?');
  await user.press(screen.getByRole('button', { name: 'Send without them this time' }));
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).not.toHaveProperty('context');
  expect(screen.queryByText('Waiting: it goes with the combined answer once it is written.')).toBeNull();
  screen.unmount();

  // Turned off in Settings: no notice, no wait.
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.enableDebate();
  agent.enableContext();
  agent.askReplies = { ...answers };
  agent.synthReply = 'hold';
  globalThis.fetch = agent.fetch;
  const user2 = await open({ carryRecord: false });
  await ask(user2, 'When is the first ferry?');
  expect(await screen.findByText('Writing the combined answer…', {}, { timeout: 5000 })).toBeTruthy();
  await follow(user2, 'And the last ferry?');
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1]).not.toHaveProperty('context');
});

test('Settings has the switch under Moderator', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/settings' });
  const sw = await screen.findByRole('switch', { name: 'Carry conclusions into follow-ups' });
  expect(sw.props.accessibilityState).toMatchObject({ checked: true });
  await user.press(sw);
  await waitFor(async () => expect((await loadAskPrefs()).carryRecord).toBe(false));
});
