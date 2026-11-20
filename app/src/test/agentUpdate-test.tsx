// An agent older than the app (11-18): the store updates the app on its
// own, the PC's agent only when the install line runs on the PC again
// (#35 brings a button later). The Ask screen and My PC say so, with the
// line to run; put away, the notice stays away for these two versions.

import { userEvent } from '@testing-library/react-native';
import Constants from 'expo-constants';
import * as Clipboard from 'expo-clipboard';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';
import { Share } from 'react-native';

import { installLine, resetAgentUpdate } from '@/api/agentUpdate';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

const appDir = './src/app';
const config = Constants.expoConfig as { version?: string } | null;
let saved: string | undefined;
let agent: FakeAgent;

beforeEach(() => {
  saved = config?.version;
  if (config) config.version = '0.4.0';
  agent = new FakeAgent();
  agent.enableAsk();
  agent.version = '0.3.4';
  globalThis.fetch = agent.fetch;
});

afterEach(() => {
  if (config) config.version = saved;
  setLang('en');
  jest.restoreAllMocks();
});

async function open(url = '/') {
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  const user = userEvent.setup();
  const r = await renderRouter(appDir, { initialUrl: url });
  return { user, r };
}

test('the Ask screen says the agent is older, and the sheet has the line to run on Windows', async () => {
  const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction', activityType: undefined });
  const { user } = await open();
  // The band under the top bar (#67), not a card in the empty screen.
  expect(await screen.findByText('New PC agent 0.4.0')).toBeTruthy();
  expect(screen.getByTestId('agent-update-band')).toBeTruthy();
  // The screen still asks: an older agent answers what it knows.
  expect(screen.getByLabelText('Ask a question')).toBeTruthy();

  await user.press(screen.getByRole('button', { name: 'How to update' }));
  expect(await screen.findByText(/^Run this line in PowerShell on the PC/)).toBeTruthy();
  expect(screen.getByText(installLine.windows)).toBeTruthy();
  expect(screen.getByText(/wait for answers to finish/)).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Copy command' }));
  expect(Clipboard.setStringAsync).toHaveBeenCalledWith(installLine.windows);
  expect(screen.getByText('Copied.')).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Send the line' }));
  expect(share).toHaveBeenCalledWith({ message: installLine.windows });
});

test('a Mac gets the terminal line', async () => {
  agent.os = 'darwin';
  const { user } = await open();
  await user.press(await screen.findByRole('button', { name: 'How to update' }));
  expect(await screen.findByText(/^Run this line in a terminal on the PC, without sudo/)).toBeTruthy();
  expect(screen.getByText(installLine.unix)).toBeTruthy();
});

test('Later puts it away for these versions, also after the app opens again; a newer app shows it again', async () => {
  const { user, r } = await open();
  await user.press(await screen.findByRole('button', { name: 'Later' }));
  await waitFor(() => expect(screen.queryByTestId('agent-update-band')).toBeNull());
  await r.unmount();

  // The app opens again: what was put away is read back from the phone.
  resetAgentUpdate();
  const again = await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByLabelText('Ask a question')).toBeTruthy();
  expect(screen.queryByTestId('agent-update-band')).toBeNull();
  await again.unmount();

  // The store brought a newer app; the agent is still the same.
  if (config) config.version = '0.4.1';
  resetAgentUpdate();
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByText('New PC agent 0.4.1')).toBeTruthy();
});

test('no notice when the agent is as new as the app, or when it is a build of the branch', async () => {
  agent.version = '0.4.0';
  const { r } = await open();
  expect(await screen.findByLabelText('Ask a question')).toBeTruthy();
  expect(screen.queryByTestId('agent-update-band')).toBeNull();
  await r.unmount();

  agent.version = '0.4.0-dev+f0a576d';
  await renderRouter(appDir, { initialUrl: '/' });
  expect(await screen.findByLabelText('Ask a question')).toBeTruthy();
  expect(screen.queryByTestId('agent-update-band')).toBeNull();
});

test('My PC shows it too, and putting it away there puts it away on the Ask screen', async () => {
  const { user } = await open('/pcs');
  expect(await screen.findByRole('header', { name: 'Update the agent on your PC' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: 'Later' }));
  await waitFor(() => expect(screen.queryByTestId('agent-update')).toBeNull());
  await user.press(screen.getByRole('tab', { name: 'All at once' }));
  expect(await screen.findByLabelText('Ask a question')).toBeTruthy();
  expect(screen.queryByTestId('agent-update-band')).toBeNull();
});

test('in Korean', async () => {
  setLang('ko');
  const { user } = await open();
  expect(await screen.findByText('PC 에이전트 새 버전 0.4.0')).toBeTruthy();
  expect(screen.getByRole('button', { name: '나중에' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '업데이트 방법' }));
  expect(await screen.findByText(/^PC의 PowerShell\(관리자 권한이 아닌 보통 창\)에서/)).toBeTruthy();
});
