// Opening again always asks the PC (2026-10-27): reusing a link after the
// session stopped showed it as archived in the Claude app.

import { userEvent } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Linking } from 'react-native';

import { ConnectionStore } from '@/connections/store';
import { FakeAgent, HOST, PORT, TOKEN, session } from '@/test/fakeAgent';

test('open again and open after stop both go through the PC', async () => {
  const agent = new FakeAgent();
  agent.sessions = [session({ id: 'claude:2', title: 'Write release notes', state: 'stopped' })];
  globalThis.fetch = agent.fetch;
  const openURL = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined);
  const c = await new ConnectionStore().add(
    { kind: 'pc', name: 'PC', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'T', pairedAt: 'x' },
    TOKEN,
  );
  const user = userEvent.setup();
  await renderRouter('./src/app', { initialUrl: `/pc/${c.id}` });
  await user.press(await screen.findByText('Write release notes'));
  const opens = () => agent.callsTo('POST', '/v1/sessions/claude%3A2/open').length;

  await user.press(await screen.findByText('Open in the Claude app'));
  await waitFor(() => expect(openURL).toHaveBeenCalledTimes(1));
  await user.press(await screen.findByText('Open again'));
  await waitFor(() => expect(opens()).toBe(2));

  await user.press(await screen.findByText('Stop'));
  expect(await screen.findByText('Stopped. Opening it again resumes it.')).toBeTruthy();
  expect(screen.queryByText('Open again')).toBeNull();
  await user.press(screen.getByText('Open in the Claude app'));
  await waitFor(() => expect(opens()).toBe(3));
  jest.restoreAllMocks();
});
