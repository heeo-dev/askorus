// P2-2: the 한 번에 묻기 tab against a pretend agent that answers asks.

import { userEvent } from '@testing-library/react-native';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;
const appDir = './src/app';

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

async function pairedPC(name = 'DESKTOP-TEST', host = HOST, token = TOKEN) {
  return new ConnectionStore().add(
    { kind: 'pc', name, host, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' },
    token,
  );
}

async function ask(user: ReturnType<typeof userEvent.setup>, question: string, button = 'Ask') {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: button }));
}

test('with no PC it says the PC answers and offers pairing', async () => {
  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByText("Your PC's AIs answer the questions.")).toBeTruthy();
  expect(screen.getByText('Pair with a PC')).toBeTruthy();
});

test('an agent without asks says to update it', async () => {
  agent = new FakeAgent(); // no ask capability
  globalThis.fetch = agent.fetch;
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByText(/does not know asks yet/)).toBeTruthy();
  expect(screen.queryByLabelText('Ask a question')).toBeNull();
});

test('one question goes to every AI; answers show as each AI finishes', async () => {
  agent.runLimit = 1;
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, '대한민국의 수도는?');

  // Claude has answered, the other two are still at it.
  expect(await screen.findByText('claude says: 대한민국의 수도는?')).toBeTruthy();
  await waitFor(() => expect(screen.getAllByText('Answering…')).toHaveLength(2));
  expect(screen.getByRole('button', { name: 'Follow up' })).toBeDisabled();

  agent.runLimit = Infinity;
  expect(await screen.findByText('opencode says: 대한민국의 수도는?')).toBeTruthy();
  expect(screen.getByText('codex says: 대한민국의 수도는?')).toBeTruthy();
  expect(screen.queryByText('Answering…')).toBeNull();
  expect(screen.getAllByText(/^1\.2 s · \w+-standard$/)).toHaveLength(3); // time, then model

  const [req] = agent.runRequests();
  expect(req).toMatchObject({ mode: 'ask', prompt: '대한민국의 수도는?', tier: 'standard' });
  expect(req.agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }, { agent: 'opencode' }]);
  expect(req.clientRunId).toMatch(/^[0-9a-f-]{36}$/);
  expect(screen.getByText('대한민국의 수도는?')).toBeTruthy(); // the question bubble
});

test('the tier, the AIs turned on and a picked model go with the question and stay for next time', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await user.press(await screen.findByRole('radio', { name: 'Deep' }));
  await user.press(screen.getByRole('switch', { name: 'Codex' })); // off
  await user.longPress(screen.getByRole('switch', { name: 'Claude' }));
  expect(await screen.findByText('Claude model')).toBeTruthy();
  await user.press(screen.getByRole('radio', { name: 'opus' }));
  expect(await screen.findByText('Claude · opus')).toBeTruthy();
  await ask(user, 'q1');
  await screen.findByText('opencode says: q1');
  expect(agent.runRequests()[0]).toMatchObject({ tier: 'deep', agents: [{ agent: 'claude', model: 'opus' }, { agent: 'opencode' }] });

  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByRole('radio', { name: 'Deep' })).toBeChecked();
  expect(screen.getByRole('switch', { name: 'Codex' })).not.toBeChecked();
  expect(screen.getByText('Claude · opus')).toBeTruthy();

  // Back to answering by tier.
  await user.longPress(screen.getByRole('switch', { name: 'Claude' }));
  await user.press(await screen.findByRole('radio', { name: 'By tier' }));
  expect(await screen.findByText('Claude')).toBeTruthy();
});

test('follow-ups carry each AI on its own conversation; "Only this AI" asks just that one', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'first');
  await screen.findByText('opencode says: first');

  const codexCard = screen.getByLabelText("Codex's answer");
  await user.press(screen.getAllByText('Only this AI')[1]);
  expect(screen.getByText('Only Codex ✕')).toBeTruthy();
  expect(codexCard).toBeTruthy();
  await ask(user, 'only you', 'Follow up');
  await screen.findByText('codex says: only you');
  expect(agent.runRequests()[1].agents).toEqual([{ agent: 'codex', session: 'codex:s-1' }]);

  // Back to everyone: each AI goes on with its latest conversation.
  await ask(user, 'all again', 'Follow up');
  await screen.findByText('opencode says: all again');
  expect(agent.runRequests()[2].agents).toEqual([
    { agent: 'claude', session: 'claude:s-1' },
    { agent: 'codex', session: 'codex:s-1' },
    { agent: 'opencode', session: 'opencode:s-1' },
  ]);

  // A new question starts new conversations.
  await user.press(screen.getByRole('button', { name: 'New question' }));
  expect(screen.queryByText('first')).toBeNull();
  await ask(user, 'fresh');
  await screen.findByText('opencode says: fresh');
  expect(agent.runRequests()[3].agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }, { agent: 'opencode' }]);
});

test('answers are drawn from their Markdown; copy keeps the Markdown', async () => {
  agent.askReplies.claude = '수도는 **서울**입니다.\n\n- 인구: 약 930만';
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(screen.queryByText(/\*\*/)).toBeNull();
  expect(screen.getByText('서울')).toHaveStyle({ fontWeight: '700' });
  expect(screen.getByText('인구: 약 930만')).toBeTruthy();
  await user.press(screen.getAllByText('Copy')[0]);
  expect(Clipboard.setStringAsync).toHaveBeenCalledWith('수도는 **서울**입니다.\n\n- 인구: 약 930만');
});

test('an AI that fails says why while the others answer', async () => {
  agent.askReplies.codex = { code: 'ask_failed', message: 'usage limit reached' };
  agent.askReplies.opencode = { code: 'ask_timeout' };
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'q');
  expect(await screen.findByText('claude says: q')).toBeTruthy();
  expect(await screen.findByText('The AI reported an error.\nusage limit reached')).toBeTruthy();
  expect(screen.getByText('No answer within 3 minutes.')).toBeTruthy();
});

test('a refused run says why, and trying again sends the same run', async () => {
  agent.runError = { status: 429, code: 'too_many_runs' };
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'q');
  expect(await screen.findByText(/The PC is answering other questions/)).toBeTruthy();
  agent.runError = null;
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  expect(await screen.findByText('claude says: q')).toBeTruthy();
  const [a, b] = agent.runRequests();
  expect(b.clientRunId).toBe(a.clientRunId);
});

test("Claude's answer continues in the Claude app; any answer copies", async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(screen.getAllByText('Continue in the Claude app')).toHaveLength(1); // Claude's card only

  await user.press(screen.getByText('Continue in the Claude app'));
  await waitFor(() => expect(open).toHaveBeenCalledWith(agent.remoteUrl));
  expect(agent.callsTo('POST', `/v1/sessions/${encodeURIComponent('claude:s-1')}/open`)).toHaveLength(1);

  await user.press(screen.getAllByText('Copy')[1]);
  expect(Clipboard.setStringAsync).toHaveBeenCalledWith('codex says: q');
  expect(await screen.findByText('Copied.')).toBeTruthy();
});

test('the ask folder that needs trusting is explained when continuing in the Claude app', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  agent.openError = { code: 'workspace_not_trusted', message: 'C:\\Users\\me\\AppData\\Local\\Asksemble\\ask' };
  await user.press(screen.getByText('Continue in the Claude app'));
  expect(await screen.findByText(/trust the ask folder on the PC once: in a PC terminal, go to C:\\Users\\me\\AppData\\Local\\Asksemble\\ask/)).toBeTruthy();
});

test('an AI the PC cannot use is dimmed and never asked', async () => {
  agent.askAgents[1] = { agent: 'opencode', available: false, capabilities: [], notices: ['opencode_not_found'] };
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByRole('switch', { name: 'OpenCode' })).toBeDisabled();
  await ask(user, 'q');
  await screen.findByText('codex says: q');
  expect(agent.runRequests()[0].agents).toEqual([{ agent: 'claude' }, { agent: 'codex' }]);
});

test('with two PCs the person picks which one answers', async () => {
  const laptop = new FakeAgent();
  laptop.enableAsk();
  const laptopHost = 'laptop.tail0.ts.net';
  globalThis.fetch = (async (u: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(u));
    if (url.hostname === laptopHost) return laptop.fetch(`https://${HOST}:${PORT}${url.pathname}${url.search}`, init);
    return agent.fetch(u, init);
  }) as typeof fetch;
  await pairedPC();
  await pairedPC('LAPTOP', laptopHost, TOKEN);
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await user.press(await screen.findByRole('radio', { name: 'LAPTOP' }));
  await ask(user, 'q');
  await screen.findByText('opencode says: q');
  expect(laptop.runRequests()).toHaveLength(1);
  expect(agent.runRequests()).toHaveLength(0);
});

test('Settings sets the default tier the ask screen starts with', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(appDir, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('radio', { name: 'Quick' }));
  await renderRouter(appDir, { initialUrl: '/ask' });
  expect(await screen.findByRole('radio', { name: 'Quick' })).toBeChecked();
});

test('Ask chats on My PCs shows only asks; the other chips leave them out', async () => {
  const user = userEvent.setup();
  const c = await pairedPC();
  await renderRouter(appDir, { initialUrl: '/ask' });
  await ask(user, 'What is TLS?');
  await screen.findByText('opencode says: What is TLS?');
  agent.sessions = [...agent.sessions, { ...agent.sessions[0], id: 'claude:code', title: 'Fix the login bug', badges: [] }];

  await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
  expect(await screen.findByText('Fix the login bug')).toBeTruthy();
  expect(screen.queryByText('What is TLS?')).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Ask chats' }));
  expect(await screen.findByText('What is TLS?')).toBeTruthy();
  expect(screen.queryByText('Fix the login bug')).toBeNull();
});
