// F8: the session sheet shows the last question and answer when the PC can
// read them (S5c: the question as a bubble, the answer under the AI's
// avatar, F-PC-Session), and goes without (showing the last prompt) when not.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

const id = 'claude:7';
let agent: FakeAgent;

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.sessions = [session({ id, title: 'Fix the cache', lastPrompt: 'why is it slow' })];
  globalThis.fetch = agent.fetch;
});

async function openSheet() {
  await new ConnectionStore().add({ kind: 'pc', name: 'PC', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'T', pairedAt: 'x' }, TOKEN);
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pcs' });
  await user.press(await screen.findByText('Fix the cache'));
  return within(await screen.findByTestId('session-sheet'));
}

const previewCalls = () => agent.callsTo('GET', '/v1/sessions/claude%3A7/preview').length;

test('the sheet shows the last question, the answer and how many tools ran', async () => {
  agent.previews[id] = {
    turns: [
      { role: 'user', text: 'why is it slow' },
      { role: 'assistant', text: 'The cache was rebuilt on every request.', tools: 3 },
    ],
    truncated: false,
  };
  const sheet = await openSheet();
  expect(await sheet.findByText('Recent conversation')).toBeTruthy();
  // Who said what is read out with each line.
  expect(sheet.getByLabelText('You asked: why is it slow')).toBeTruthy();
  expect(sheet.getByLabelText('Claude: The cache was rebuilt on every request.')).toBeTruthy();
  expect(sheet.getByTestId('avatar-claude', { includeHiddenElements: true })).toBeTruthy();
  expect(sheet.getByText('Used tools 3 times')).toBeTruthy();
  // The preview replaces the short last-prompt block.
  expect(sheet.queryByText('Last prompt')).toBeNull();
  expect(previewCalls()).toBe(1);
});

test('tools are shown by kind, most used first', async () => {
  agent.previews[id] = {
    turns: [
      { role: 'user', text: 'fix it' },
      {
        role: 'assistant',
        text: 'Fixed.',
        tools: 6,
        toolKinds: [
          { kind: 'run', count: 3 },
          { kind: 'read', count: 2 },
          { kind: 'something-new', count: 1 },
        ],
      },
    ],
    truncated: false,
  };
  const sheet = await openSheet();
  // A kind this app does not know yet shows its code rather than nothing.
  expect(await sheet.findByText('Tools: ran commands 3 · read files 2 · something-new 1')).toBeTruthy();
  expect(sheet.queryByText('Used tools 6 times')).toBeNull();
});

test('a question without an answer yet says so', async () => {
  agent.previews[id] = { turns: [{ role: 'user', text: 'still thinking?' }], truncated: false };
  const sheet = await openSheet();
  expect(await sheet.findByText('still thinking?')).toBeTruthy();
  expect(sheet.getByText('No answer yet.')).toBeTruthy();
});

test('when the PC cannot read it, the sheet goes without', async () => {
  agent.previews[id] = 'fail';
  const sheet = await openSheet();
  await waitFor(() => expect(previewCalls()).toBe(1));
  expect(await sheet.findByText('Last prompt')).toBeTruthy();
  expect(sheet.getByText('why is it slow')).toBeTruthy(); // the last prompt as before
  expect(sheet.queryByText('Recent conversation')).toBeNull();
  expect(sheet.queryByText(/internal/)).toBeNull();
});

test('an older agent without preview is not asked', async () => {
  agent.capabilities = agent.capabilities.filter((c) => c !== 'preview');
  agent.previews[id] = { turns: [{ role: 'user', text: 'never shown' }], truncated: false };
  const sheet = await openSheet();
  expect(await sheet.findByText('why is it slow')).toBeTruthy();
  expect(previewCalls()).toBe(0);
  expect(sheet.queryByText('never shown')).toBeNull();
});
