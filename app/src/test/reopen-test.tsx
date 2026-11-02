// Opening again always asks the PC (2026-10-27): reusing a link after the
// session stopped showed it as archived in the Claude app.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor, within } from 'expo-router/testing-library';
import { Linking, Text } from 'react-native';

import TabsLayout from '@/app/(tabs)/_layout';
import MyPC from '@/app/(tabs)/pcs';
import RootLayout from '@/app/_layout';
import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

const routes = {
  _layout: RootLayout,
  '(tabs)/_layout': TabsLayout,
  '(tabs)/index': () => <Text>ask screen</Text>,
  '(tabs)/pcs': MyPC,
};

test('open again and open after stop both go through the PC', async () => {
  const agent = new FakeAgent();
  agent.sessions = [session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' })];
  globalThis.fetch = agent.fetch;
  const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  await new ConnectionStore().add({ kind: 'pc', name: 'PC', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'T', pairedAt: 'x' }, TOKEN);
  const user = userEvent.setup();
  await renderRouter(routes, { initialUrl: '/pcs' });
  await user.press(await screen.findByText('Write release notes'));
  const sheet = within(await screen.findByTestId('session-sheet'));
  const opens = () => agent.callsTo('POST', '/v1/sessions/claude%3A2/open').length;

  await user.press(sheet.getByRole('button', { name: 'Open in the Claude app' }));
  await waitFor(() => expect(openURL).toHaveBeenCalledTimes(1));
  await user.press(await sheet.findByRole('button', { name: 'Open again' }));
  await waitFor(() => expect(opens()).toBe(2));

  // Stop waits, greyed, until the PC has answered the open.
  await waitFor(() => expect(sheet.getByRole('button', { name: 'Stop' })).toBeEnabled());
  await user.press(sheet.getByRole('button', { name: 'Stop' }));
  expect(await sheet.findByText('Stopped. Opening it again resumes it.')).toBeTruthy();
  expect(sheet.queryByRole('button', { name: 'Open again' })).toBeNull();
  await user.press(sheet.getByRole('button', { name: 'Open in the Claude app' }));
  await waitFor(() => expect(opens()).toBe(3));
  jest.restoreAllMocks();
});
