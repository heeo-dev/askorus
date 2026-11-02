// Folders with past conversations (기록 폴더): offered for new sessions
// under their own label next to the allowed ones (S5c, F-PC-New), and
// saved to the allowed list when kept open.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking, Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import Folders from '@/app/pc/[id]/folders';
import NewSession from '@/app/pc/[id]/new';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

let agent: FakeAgent;

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
  'pc/[id]/new': NewSession,
  'pc/[id]/folders': Folders,
};

beforeEach(() => {
  agent = new FakeAgent();
  agent.folders = [
    { id: 'shop', name: 'shop', path: 'D:\\work\\shop', isGit: true, source: 'config', server: { enabled: false, capacity: 4 } },
    {
      id: 'h-0123456789ab',
      name: 'blog',
      path: 'D:\\work\\blog',
      isGit: false,
      source: 'history',
      lastActive: new Date(Date.now() - 3 * 86400_000).toISOString(),
      server: { enabled: false, capacity: 4 },
      status: { state: 'off', sessions: 0 },
    },
  ];
  globalThis.fetch = agent.fetch;
});

afterEach(() => jest.restoreAllMocks());

async function pairedPC() {
  return new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
}

test('new session lists allowed and recent folders apart, and starts in a recent one', async () => {
  jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const user = userEvent.setup();
  const c = await pairedPC();
  await renderRouter(routes, { initialUrl: `/pc/${c.id}/new` });
  expect(await screen.findByRole('radio', { name: 'shop' })).toBeTruthy();
  expect(screen.getByText('Folder')).toBeTruthy();
  expect(screen.getByText('Folders with recent conversations')).toBeTruthy();
  expect(screen.getByText(/^3 d ago · D:\\work\\blog$/)).toBeTruthy();
  expect(screen.getByText('D:\\work\\shop')).toBeTruthy();

  await user.press(screen.getByRole('radio', { name: 'blog' }));
  await user.press(screen.getByRole('button', { name: 'Start and open in the Claude app' }));
  await waitFor(() => expect(agent.callsTo('POST', '/v1/sessions')).toHaveLength(1));
  expect(agent.callsTo('POST', '/v1/sessions')[0].body.folder).toBe('h-0123456789ab');
});

test('keeping a recent folder open saves it to the allowed list, and My PC lists it', async () => {
  const user = userEvent.setup();
  await pairedPC();
  await renderRouter(routes, { initialUrl: '/pcs' });
  await user.press(await screen.findByRole('button', { name: 'Keep folders open' }));
  expect(await screen.findByText('Turning one on saves it to the allowed list on the PC and keeps it open.')).toBeTruthy();
  await user.press(screen.getByRole('switch', { name: 'Keep blog open' }));
  await waitFor(() => expect(screen.queryByText('Folders with recent conversations')).toBeNull());
  expect(screen.getByText('Open · 0/4 sessions')).toBeTruthy();
  expect(agent.folders.find((f) => f.path === 'D:\\work\\blog')?.source).toBe('config');

  // Back on My PC the folder shows as kept open.
  await user.press(screen.getByRole('button', { name: 'Back' }));
  expect(await screen.findByRole('button', { name: 'blog, Open' })).toBeTruthy();
});
