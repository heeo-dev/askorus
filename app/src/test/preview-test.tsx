// F8: the session sheet shows the last question and answer when the PC can
// read them, and goes without (showing the last prompt as before) when not.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

const id = 'claude:7';
let agent: FakeAgent;

beforeEach(() => {
  agent = new FakeAgent();
  agent.sessions = [session({ id, title: 'Fix the cache', lastPrompt: 'why is it slow' })];
  globalThis.fetch = agent.fetch;
});

async function openSheet() {
  const c = await new ConnectionStore().add(
    { kind: 'pc', name: 'PC', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'T', pairedAt: 'x' },
    TOKEN,
  );
  const user = userEvent.setup();
  await renderRouter('./src/app', { initialUrl: `/pc/${c.id}` });
  await user.press(await screen.findByText('Fix the cache'));
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
  await openSheet();
  expect(await screen.findByText('Recent conversation')).toBeTruthy();
  expect(screen.getByText('You asked')).toBeTruthy();
  expect(screen.getByText('The cache was rebuilt on every request.')).toBeTruthy();
  expect(screen.getByText('Used tools 3 times')).toBeTruthy();
  // The preview replaces the short last-prompt row.
  expect(screen.queryByText('Last prompt')).toBeNull();
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
  await openSheet();
  // A kind this app does not know yet shows its code rather than nothing.
  expect(await screen.findByText('Tools: ran commands 3 · read files 2 · something-new 1')).toBeTruthy();
  expect(screen.queryByText('Used tools 6 times')).toBeNull();
});

test('a question without an answer yet says so', async () => {
  agent.previews[id] = { turns: [{ role: 'user', text: 'still thinking?' }], truncated: false };
  await openSheet();
  expect(await screen.findByText('still thinking?')).toBeTruthy();
  expect(screen.getByText('No answer yet.')).toBeTruthy();
});

test('when the PC cannot read it, the sheet goes without', async () => {
  agent.previews[id] = 'fail';
  await openSheet();
  await waitFor(() => expect(previewCalls()).toBe(1));
  expect(await screen.findByText('why is it slow')).toBeTruthy(); // the last prompt row as before
  expect(screen.queryByText('Recent conversation')).toBeNull();
  expect(screen.queryByText(/internal/)).toBeNull();
});

test('an older agent without preview is not asked', async () => {
  agent.capabilities = agent.capabilities.filter((c) => c !== 'preview');
  agent.previews[id] = { turns: [{ role: 'user', text: 'never shown' }], truncated: false };
  await openSheet();
  expect(await screen.findByText('why is it slow')).toBeTruthy();
  expect(previewCalls()).toBe(0);
  expect(screen.queryByText('never shown')).toBeNull();
});
