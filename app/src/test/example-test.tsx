// The example conversation (#51, '/example'): with no PC paired the first
// screen offers it, and it shows a whole conversation (the question, three
// AIs' answers, the combined answer, a debate) from what the app carries.
// What needs a PC is not offered, nothing is asked of any PC and nothing is
// kept in the history. An answer can be opened, copied, shared and reported
// (#50), which is what a store reviewer without a PC has to be able to see.

import { userEvent, within } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking, Share } from 'react-native';

import { conversations } from '@/ask/conversations';
import { exampleDate, exampleTurns } from '@/ask/example';
import { reportAddress } from '@/ask/report';
import { setLang } from '@/i18n';

const appDir = './src/app';
let fetched: jest.Mock;

beforeEach(() => {
  // No PC is paired, and the example must not look for one.
  fetched = jest.fn(async () => {
    throw new Error('the example asked the network for something');
  });
  globalThis.fetch = fetched as unknown as typeof fetch;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
  jest.clearAllMocks();
});

async function openExample() {
  const user = userEvent.setup();
  const r = await renderRouter(appDir, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: 'See an example' }));
  await waitFor(() => expect(r.getPathname()).toBe('/example'));
  return { user, r };
}

test('with no PC the first screen offers an example, next to pairing', async () => {
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText("Your PC's AIs answer the questions.")).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Pair with a PC' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'See an example' })).toBeTruthy();
});

test('the example is a whole conversation, marked as an example, with nothing that needs a PC', async () => {
  const [turn] = exampleTurns('en');
  const { user, r } = await openExample();

  expect(screen.getByRole('header', { name: 'Example' })).toBeTruthy();
  // Always in view: it is an example, and the answers are real.
  expect(screen.getByText(`This is an example. The answers are real: they were received with this app on ${exampleDate.en}.`)).toBeTruthy();

  expect(screen.getByText(turn.question)).toBeTruthy();
  expect(screen.getByText('Combined answer')).toBeTruthy();
  expect(screen.getAllByText(/^An AI wrote this/).length).toBeGreaterThan(0);
  // The three AIs' own answers, as cards.
  for (const name of ['Claude', 'Codex', 'OpenCode']) expect(screen.getByRole('button', { name: new RegExp(`^${name}'s answer\\.`) })).toBeTruthy();
  // The debate was held; its answer is there.
  expect(screen.getByText('After the debate')).toBeTruthy();

  // Nothing to ask with, and no debate to start: both need a PC.
  expect(screen.queryByLabelText('Follow up')).toBeNull();
  expect(screen.queryByLabelText('Ask a question')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Debate' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Debate again' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'New question' })).toBeNull();
  expect(screen.getByText('Pair a PC to ask your own questions.')).toBeTruthy();

  // No PC was asked anything, and the example is not in the history.
  expect(fetched).not.toHaveBeenCalled();
  expect(conversations.list()).toEqual([]);

  await user.press(screen.getByRole('button', { name: 'Pair with a PC' }));
  await waitFor(() => expect(r.getPathname()).toBe('/pair'));
});

test("an AI's answer opens in full with Copy and Share or report, without the actions that need a PC", async () => {
  const [turn] = exampleTurns('en');
  const claude = turn.run?.parts.find((p) => p.agent === 'claude');
  const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction', activityType: undefined });
  const { user } = await openExample();

  await user.press(screen.getByRole('button', { name: /^Claude's answer\./ }));
  const sheet = within(await screen.findByTestId('answer-sheet'));
  expect(sheet.getByRole('header', { name: 'Claude' })).toBeTruthy();
  expect(sheet.queryByRole('button', { name: 'Only this AI' })).toBeNull();
  expect(sheet.queryByRole('button', { name: 'Continue in the Claude app' })).toBeNull();

  await user.press(sheet.getByRole('button', { name: 'Copy' }));
  expect((Clipboard.setStringAsync as jest.Mock).mock.calls.at(-1)?.[0]).toBe(claude?.answer);
  expect(sheet.getByText('Copied.')).toBeTruthy();

  await user.press(sheet.getByRole('button', { name: 'Share or report' }));
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Share' }));
  expect(share).toHaveBeenCalledWith({ message: claude?.answer });
  expect(fetched).not.toHaveBeenCalled();
});

test('an answer of the example can be reported like any other: the mail app opens with the report (#50)', async () => {
  const [turn] = exampleTurns('en');
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const { user } = await openExample();

  // The combined answer's own '...' is the first on the screen.
  await user.press(screen.getAllByRole('button', { name: 'Share or report' })[0]);
  await user.press(within(screen.getByTestId('answer-more')).getByRole('button', { name: 'Report' }));
  const sheet = within(screen.getByTestId('answer-report'));
  await user.press(sheet.getByRole('switch', { name: 'Include the question and the answer' }));
  await user.press(sheet.getByRole('radio', { name: 'Something else' }));
  await user.press(sheet.getByRole('button', { name: 'Open mail app' }));

  expect(open).toHaveBeenCalledTimes(1);
  const link = open.mock.calls[0][0];
  expect(link.startsWith(`mailto:${reportAddress}?`)).toBe(true);
  const body = decodeURIComponent(link.slice(link.indexOf('&body=') + 6));
  expect(body).toContain('Written by: Combined answer by Claude');
  expect(body).toContain(turn.question);
  expect(screen.getByText('The report is sent when you press Send in your mail app.')).toBeTruthy();
});

test('the list of the AIs that answered opens, and leads to an answer', async () => {
  const { user } = await openExample();
  // The combined answer's pile of avatars (the debate has one of its own below).
  await user.press(screen.getAllByRole('button', { name: /^AIs that answered: 3/ })[0]);
  const list = within(await screen.findByTestId('ai-sheet'));
  expect(list.getByRole('header', { name: '3 AIs' })).toBeTruthy();
  await user.press(list.getByText('Codex'));
  expect(within(await screen.findByTestId('answer-sheet')).getByRole('header', { name: 'Codex' })).toBeTruthy();
});

test('in Korean the example is the Korean one', async () => {
  setLang('ko');
  const [turn] = exampleTurns('ko');
  const user = userEvent.setup();
  const r = await renderRouter(appDir, { initialUrl: '/' });
  await user.press(await screen.findByRole('button', { name: '예시 보기' }));
  await waitFor(() => expect(r.getPathname()).toBe('/example'));
  expect(screen.getByRole('header', { name: '예시' })).toBeTruthy();
  expect(screen.getByText(`예시입니다. ${exampleDate.ko}에 이 앱으로 실제로 받은 답입니다.`)).toBeTruthy();
  expect(screen.getByText(turn.question)).toBeTruthy();
  expect(screen.getByText('PC와 짝을 맺으면 직접 물을 수 있습니다.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'PC와 짝 맺기' })).toBeTruthy();
});

test('opened with nothing behind it, Back leads to the first screen', async () => {
  const user = userEvent.setup();
  const r = await renderRouter(appDir, { initialUrl: '/example' });
  await user.press(await screen.findByRole('button', { name: 'Back' }));
  await waitFor(() => expect(r.getPathname()).toBe('/'));
});
