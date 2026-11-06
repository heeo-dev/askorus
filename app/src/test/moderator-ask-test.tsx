// #25 사회자에게 묻기: the follow-up picker's '사회자' row, against a pretend
// agent that asks the moderator (fakeAgent enableModerator). The row only
// where the PC asks the moderator; the question goes to the moderator AI
// alone with the record and the last answers; its one card is the
// moderator's (no combined answer, no debate, no Only this AI); the next
// follow-up starts with the AIs again; the record goes even with the
// setting off; Korean. Every user event is awaited (R-07).

import { userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const answers = {
  claude: 'Water boils at 100 °C at sea level.',
  codex: 'Water boils at 90 °C at sea level.',
  opencode: 'At sea level water boils at 100 °C.',
};

function pretend(moderator = true) {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableSynth();
  agent.enableDebate();
  agent.enableContext();
  if (moderator) agent.enableModerator();
  agent.askReplies = { ...answers };
  globalThis.fetch = agent.fetch;
}

beforeEach(() => pretend());

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

const pairedPC = () =>
  new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await pairedPC();
  await saveAskPrefs({ ...defaultAskPrefs, recordNoticed: true, ...prefs });
  await renderRouter(appDir, { initialUrl: '/' });
  return user;
}

async function firstQuestion(user: User) {
  await user.type(await screen.findByLabelText('Ask a question'), 'At what temperature does water boil?');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(1), { timeout: 5000 });
}

const pile = () => screen.getByRole('button', { name: 'AIs for the follow-up: 3 (Claude, Codex, OpenCode). Pick' });

async function pickModerator(user: User) {
  await user.press(pile());
  await user.press(await screen.findByRole('switch', { name: /^Moderator, Claude · reads the conversation/ }));
  await user.press(screen.getByLabelText('Close'));
}

test('the moderator answers alone, from the record and the last answers, in one card', async () => {
  const user = await open();
  await firstQuestion(user);
  await pickModerator(user);
  const bar = within(screen.getByTestId('follow-bar'));
  expect(bar.getByText('To the moderator')).toBeTruthy();
  await user.type(screen.getByLabelText('Ask the moderator'), 'What did the AIs differ on?');
  await user.press(screen.getByRole('button', { name: 'Send to the moderator' }));
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  const req = agent.runRequests()[1];
  expect(req).toMatchObject({
    prompt: 'What did the AIs differ on?',
    agents: [{ agent: 'claude' }],
    moderator: {
      agent: 'claude',
      answers: [
        { agent: 'claude', text: answers.claude },
        { agent: 'codex', text: answers.codex },
        { agent: 'opencode', text: answers.opencode },
      ],
    },
    context: { earlier: ['At what temperature does water boil?'], last: expect.stringMatching(/^Fake combined answer\./) },
  });
  expect(req.compare).toBeUndefined();
  expect(req.agents[0].session).toBeUndefined();

  // One card, the moderator's: no combined answer, no debate, no Only this AI.
  expect(await screen.findByText('Moderator · Claude', {}, { timeout: 5000 })).toBeTruthy();
  expect(screen.getByLabelText("Moderator · Claude's answer")).toBeTruthy();
  // Its answer comes in (the fake moderator answers as Claude does).
  await waitFor(() => expect(within(screen.getByLabelText("Moderator · Claude's answer")).getByText(answers.claude)).toBeTruthy(), { timeout: 5000 });
  expect(screen.getAllByText('Fake combined answer.')).toHaveLength(1);
  expect(screen.getAllByRole('button', { name: 'Debate' })).toHaveLength(1);
  expect(screen.getByRole('button', { name: 'Builds on earlier conclusions. Show' })).toBeTruthy();

  // The next follow-up starts with the AIs again, each in its own conversation.
  expect(pile()).toBeTruthy();
  await user.type(screen.getByLabelText('Follow up'), 'And at 3,000 m?');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await waitFor(() => expect(agent.runRequests()).toHaveLength(3));
  expect(agent.runRequests()[2].agents.map((a: { agent: string; session?: string }) => [a.agent, !!a.session])).toEqual([
    ['claude', true],
    ['codex', true],
    ['opencode', true],
  ]);
  expect(agent.runRequests()[2].moderator).toBeUndefined();
});

test('the moderator reads the record even with the setting off; its chip can be cancelled; picking an AI leaves it', async () => {
  const user = await open({ carryRecord: false, recordNoticed: false });
  await firstQuestion(user);
  await pickModerator(user);
  // Cancelled: the AIs again.
  await user.press(screen.getByRole('button', { name: 'Stop asking the moderator' }));
  expect(pile()).toBeTruthy();
  // Picking an AI in the sheet while the moderator is picked: that AI alone.
  await pickModerator(user);
  await user.press(screen.getByRole('button', { name: 'To the moderator' }));
  await user.press(await screen.findByRole('switch', { name: /^Codex, / }));
  await user.press(screen.getByLabelText('Close'));
  expect(within(screen.getByTestId('follow-bar')).getByText('Only Codex')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Stop asking only Codex' }));

  await pickModerator(user);
  await user.type(screen.getByLabelText('Ask the moderator'), 'Who was right?');
  await user.press(screen.getByRole('button', { name: 'Send to the moderator' }));
  // No notice: its company already gets every answer.
  expect(screen.queryByRole('header', { name: 'Earlier conclusions go along' })).toBeNull();
  await waitFor(() => expect(agent.runRequests()).toHaveLength(2));
  expect(agent.runRequests()[1].context).toMatchObject({ last: expect.stringMatching(/^Fake combined answer\./) });
});

test('no moderator row where the PC does not ask the moderator', async () => {
  pretend(false);
  const user = await open();
  await firstQuestion(user);
  await user.press(pile());
  await screen.findByRole('header', { name: 'AIs for the follow-up' });
  expect(screen.queryByRole('switch', { name: /^Moderator,/ })).toBeNull();
});

test('in Korean the moderator row, chip and card read in Korean', async () => {
  setLang('ko');
  const user = await open();
  await user.type(await screen.findByLabelText('질문을 적으세요'), '물은 몇 도에서 끓어?');
  await user.press(screen.getByRole('button', { name: '묻기' }));
  await waitFor(() => expect(screen.getAllByText('Fake combined answer.')).toHaveLength(1), { timeout: 5000 });
  await user.press(screen.getByRole('button', { name: /^이어서 물을 AI: 3개/ }));
  await user.press(await screen.findByRole('switch', { name: /^사회자, Claude · 대화를 읽고 혼자 답합니다/ }));
  await user.press(screen.getByLabelText('닫기'));
  expect(within(screen.getByTestId('follow-bar')).getByText('사회자에게')).toBeTruthy();
  await user.type(screen.getByLabelText('사회자에게 묻기'), '뭐가 달랐지?');
  await user.press(screen.getByRole('button', { name: '사회자에게 보내기' }));
  expect(await screen.findByText('사회자 · Claude', {}, { timeout: 5000 })).toBeTruthy();
});
