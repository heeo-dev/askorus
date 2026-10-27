// Screen tests: the real screens in src/app, driven like a person would,
// against a pretend PC agent.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Alert, Linking } from 'react-native';

import { ConnectionStore } from '@/connections/store';
import { CODE, FakeAgent, HOST, PAIR_LINK, PORT, TOKEN, session } from '@/test/fakeAgent';

let agent: FakeAgent;

beforeEach(() => {
  agent = new FakeAgent();
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

async function pairedPC() {
  return new ConnectionStore().add(
    {
      kind: 'pc',
      name: 'DESKTOP-TEST',
      host: HOST,
      port: PORT,
      deviceId: 'dev-1',
      deviceName: 'Test Phone',
      pairedAt: '2026-10-27T00:00:00Z',
    },
    TOKEN,
  );
}

const appDir = './src/app';

describe('home', () => {
  test('with no PC it explains how to pair', async () => {
    await renderRouter(appDir, { initialUrl: '/' });
    expect(await screen.findByText('No PC is paired yet.')).toBeTruthy();
    expect(screen.getByText('Not an official Anthropic product.')).toBeTruthy();
  });

  test('a paired PC shows as connected', async () => {
    await pairedPC();
    await renderRouter(appDir, { initialUrl: '/' });
    expect(await screen.findByText('DESKTOP-TEST')).toBeTruthy();
    expect(await screen.findByText('Connected · agent 0.1.0')).toBeTruthy();
  });

  test('a PC that dropped this phone says so', async () => {
    await pairedPC();
    agent.tokenValid = false;
    await renderRouter(appDir, { initialUrl: '/' });
    expect(await screen.findByText('This phone is no longer paired with the PC. Pair again.')).toBeTruthy();
  });
});

describe('pairing', () => {
  test('pasting the link pairs, saves the PC and goes home', async () => {
    const user = userEvent.setup();
    await renderRouter(appDir, { initialUrl: '/' });
    await user.press(await screen.findByText('Pair with a PC'));
    await user.type(await screen.findByPlaceholderText('asksemble://pair?…'), PAIR_LINK);
    await user.press(screen.getByText('Pair with this link'));

    // The phone shows which PC before sending anything.
    expect(await screen.findByText(HOST)).toBeTruthy();
    expect(agent.calls).toHaveLength(0);

    await user.press(screen.getByText('Pair'));
    expect(await screen.findByText('Connected · agent 0.1.0')).toBeTruthy();
    expect(screen).toHavePathname('/');
    expect(agent.callsTo('POST', '/v1/pair')[0].body).toEqual({ code: CODE, deviceName: 'Test Phone' });

    const [saved] = await new ConnectionStore().list();
    expect(saved).toMatchObject({ host: HOST, port: PORT, name: 'DESKTOP-TEST' });
    expect(await new ConnectionStore().token(saved.id)).toBe(TOKEN);
  });

  test('the pairing link itself opens the confirm step', async () => {
    await renderRouter(appDir, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    expect(await screen.findByText('Is this your PC?')).toBeTruthy();
    expect(screen.getByText(HOST)).toBeTruthy();
  });

  test('a refusal on the PC is explained and nothing is saved', async () => {
    const user = userEvent.setup();
    agent.pairAnswer = 'deny';
    await renderRouter(appDir, { initialUrl: `/pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}` });
    await user.press(await screen.findByText('Pair'));
    expect(await screen.findByText('Declined on the PC.')).toBeTruthy();
    expect(await new ConnectionStore().list()).toEqual([]);
  });

  test('a link that is not a pairing link is refused', async () => {
    const user = userEvent.setup();
    await renderRouter(appDir, { initialUrl: '/pair' });
    await user.type(await screen.findByPlaceholderText('asksemble://pair?…'), 'https://example.com/');
    await user.press(screen.getByText('Pair with this link'));
    expect(await screen.findByText('Not a pairing link. Paste the whole link shown on the PC.')).toBeTruthy();
  });
});

describe('sessions', () => {
  beforeEach(() => {
    agent.sessions = [
      session({ id: 'claude:1', title: 'Fix the login bug', state: 'running', folder: 'D:\\work\\shop', badges: ['rc'] }),
      session({ id: 'claude:2', title: 'Write release notes', state: 'stopped', folder: 'D:\\work\\docs' }),
      session({
        id: 'claude:3',
        title: 'Old experiment',
        state: 'history',
        folder: 'D:\\gone',
        badges: ['folder_missing'],
        openModes: [],
      }),
    ];
  });

  async function openPC() {
    const c = await pairedPC();
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
    await screen.findByText('Fix the login bug');
  }

  test('lists sessions with state, folder and badges', async () => {
    await openPC();
    expect(screen.getByText('Write release notes')).toBeTruthy();
    expect(screen.getAllByText('Running')).toHaveLength(2); // the filter chip and the row
    expect(screen.getByText('Remote on')).toBeTruthy();
    expect(screen.getByText('Folder gone')).toBeTruthy();
    expect(screen.getAllByText(/^shop · /)).toHaveLength(1);
  });

  test('the state filter asks the PC for that state only', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByRole('button', { name: 'Stopped' }));
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());
    expect(screen.getByText('Write release notes')).toBeTruthy();
    expect(agent.calls.at(-1)?.query.get('state')).toBe('stopped');
  });

  test('search waits for typing to settle, then asks the PC', async () => {
    const user = userEvent.setup();
    await openPC();
    const before = agent.callsTo('GET', '/v1/sessions').length;
    await user.type(screen.getByPlaceholderText('Search titles, folders, last prompts'), 'release');
    await waitFor(() => expect(screen.queryByText('Fix the login bug')).toBeNull());
    const searches = agent.callsTo('GET', '/v1/sessions').slice(before);
    expect(searches.at(-1)?.query.get('q')).toBe('release');
    expect(searches.length).toBeLessThan('release'.length); // not one request per letter
  });

  test('opening a stopped session hands the link to the Claude app', async () => {
    const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    await user.press(await screen.findByText('Open in the Claude app'));
    await waitFor(() => expect(open).toHaveBeenCalledWith(agent.remoteUrl));
    expect(await screen.findByText('Woke the stopped session.')).toBeTruthy();
    expect(screen.getByText('Open again')).toBeTruthy();
    expect(agent.callsTo('POST', '/v1/sessions/claude%3A2/open')).toHaveLength(1);
  });

  test('stop is offered only for running sessions', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    expect(screen.queryByText('Stop')).toBeNull();
    await user.press(screen.getByLabelText('Close'));
    await user.press(screen.getByText('Fix the login bug'));
    await user.press(await screen.findByText('Stop'));
    expect(await screen.findByText('Stopped. Opening it again resumes it.')).toBeTruthy();
    expect(agent.sessions.find((s) => s.id === 'claude:1')?.state).toBe('stopped');
  });

  test('remove asks first, then drops the session from the list', async () => {
    const alert = jest.spyOn(Alert, 'alert').mockImplementation((_t, _m, buttons) => {
      buttons?.find((b) => b.style === 'destructive')?.onPress?.();
    });
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Write release notes'));
    await user.press(await screen.findByText('Remove'));
    expect(alert).toHaveBeenCalledWith('Remove this session?', expect.any(String), expect.any(Array));
    await waitFor(() => expect(screen.queryByText('Write release notes')).toBeNull());
    expect(agent.sessions.map((s) => s.id)).toEqual(['claude:1', 'claude:3']);
  });

  test('a session whose folder is gone explains why it cannot open', async () => {
    const user = userEvent.setup();
    await openPC();
    await user.press(screen.getByText('Old experiment'));
    expect(await screen.findByText(/folder was deleted from the PC/)).toBeTruthy();
    expect(screen.queryByText('Open in the Claude app')).toBeNull();
    expect(screen.getByText('Remove')).toBeTruthy();
  });

  test('a dropped phone is offered pairing again', async () => {
    const c = await pairedPC();
    agent.tokenValid = false;
    await renderRouter(appDir, { initialUrl: `/pc/${c.id}` });
    expect(await screen.findByText('Pair again')).toBeTruthy();
  });
});
