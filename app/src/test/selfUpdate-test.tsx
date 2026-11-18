// The agent update from the phone (#35, S5): Settings' PC sheet shows the
// PC's version and the new one, what an update cuts off, the go, and what
// came of it; the old-agent card offers it when the agent can update
// itself; My PCs also asks for the latest release.

import { userEvent, within } from '@testing-library/react-native';
import Constants from 'expo-constants';
import { renderRouter, screen } from 'expo-router/testing-library';

import { followTiming } from '@/api/selfUpdate';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';

const appDir = './src/app';
const config = Constants.expoConfig as { version?: string } | null;
let saved: string | undefined;
let agent: FakeAgent;
const savedEvery = followTiming.every;

beforeEach(() => {
  saved = config?.version;
  agent = new FakeAgent();
  agent.enableAsk();
  agent.version = '0.5.0';
  agent.enableUpdate('0.5.1');
  globalThis.fetch = agent.fetch;
  followTiming.every = 5;
});

afterEach(() => {
  if (config) config.version = saved;
  followTiming.every = savedEvery;
  setLang('en');
  jest.restoreAllMocks();
});

async function paired() {
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
}

async function pcSheet() {
  await paired();
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('button', { name: 'DESKTOP-TEST, Connected' }));
  const sheet = within(await screen.findByTestId('pc-sheet'));
  return { user, sheet };
}

test('the PC sheet shows the new version and what an update cuts off, and the update goes through', async () => {
  agent.updateBusy = { asks: 1, folders: 2 };
  const { user, sheet } = await pcSheet();
  expect(await sheet.findByText('Agent update')).toBeTruthy();
  expect(await sheet.findByText('Agent 0.5.0 → new version 0.5.1')).toBeTruthy();
  expect(sheet.getByText('1 question(s) still being answered are waited for (up to 10 minutes).')).toBeTruthy();
  expect(sheet.getByText('The remote sessions of 2 folder(s) kept open drop for a moment.')).toBeTruthy();
  expect(sheet.getByText(/^The phone loses the PC for a moment/)).toBeTruthy();

  await user.press(sheet.getByRole('button', { name: 'Update now' }));
  expect(await sheet.findByText('The agent is now 0.5.1.')).toBeTruthy();
  expect(agent.updateCalls).toContain('POST 0.5.1');
  expect(agent.version).toBe('0.5.1');
  expect(sheet.queryByRole('button', { name: 'Update now' })).toBeNull();
});

test('an update that did not come up says the old version is back, and can be checked again', async () => {
  agent.updateEnd = 'rolled-back';
  const { user, sheet } = await pcSheet();
  await user.press(await sheet.findByRole('button', { name: 'Update now' }));
  expect(await sheet.findByText(/^The new version did not come up, so the old one is back/)).toBeTruthy();
  expect(sheet.getByText('it did not come up within a minute')).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: 'Check again' }));
  expect(await sheet.findByText('Agent 0.5.0 → new version 0.5.1')).toBeTruthy();
});

test('a release that changed meanwhile is shown again instead of taken', async () => {
  const { user, sheet } = await pcSheet();
  const go = await sheet.findByRole('button', { name: 'Update now' });
  agent.updateLatest = '0.5.2';
  await user.press(go);
  expect(await sheet.findByText('The new version changed meanwhile. Here is the one there is now.')).toBeTruthy();
  expect(await sheet.findByText('Agent 0.5.0 → new version 0.5.2')).toBeTruthy();
  expect(agent.version).toBe('0.5.0');
});

test('no button when the phone may not, the agent cannot, or updates are off; each says why', async () => {
  const cases: [() => void, RegExp][] = [
    [() => (agent.update = { on: true, can: true, allowed: false }), /askorus-agent devices update dev-1 on$/],
    [() => (agent.update = { on: true, can: false, why: 'not_installed', allowed: true }), /not where the install script puts it/],
    [() => (agent.update = { on: true, can: false, why: 'no_key', allowed: true }), /without an update key/],
    [() => (agent.update = { on: false, can: false, why: 'off', allowed: true }), /askorus-agent update on$/],
    [() => (agent.updateCheckError = 'no_release_info'), /no signed update information/],
  ];
  for (const [change, words] of cases) {
    agent = new FakeAgent();
    agent.enableAsk();
    agent.version = '0.5.0';
    agent.enableUpdate('0.5.1');
    change();
    globalThis.fetch = agent.fetch;
    const { sheet } = await pcSheet();
    expect(await sheet.findByText(words)).toBeTruthy();
    expect(sheet.queryByRole('button', { name: 'Update now' })).toBeNull();
    screen.unmount();
    await new ConnectionStore().remove((await new ConnectionStore().list())[0].id);
  }
});

test('the agent up to date says so; an agent from before updates has no update part', async () => {
  agent.updateLatest = '0.5.0';
  const { sheet } = await pcSheet();
  expect(await sheet.findByText('The agent on this PC is up to date (0.5.0).')).toBeTruthy();
  screen.unmount();
  await new ConnectionStore().remove((await new ConnectionStore().list())[0].id);

  agent = new FakeAgent();
  agent.enableAsk();
  globalThis.fetch = agent.fetch;
  const old = await pcSheet();
  expect(await old.sheet.findByText('This phone: Test Phone (id dev-1)')).toBeTruthy();
  expect(old.sheet.queryByText('Agent update')).toBeNull();
  expect(agent.updateCalls).toEqual([]);
});

// The old-agent card (0.4.0): an agent that can update itself is updated
// from it; the Ask screen asks GitHub nothing until the person asks.
test('the old-agent card on the Ask screen updates an agent that can update itself', async () => {
  if (config) config.version = '0.5.1';
  await paired();
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/' });
  const card = within(await screen.findByTestId('agent-update'));
  expect(agent.updateCalls).toEqual([]);
  await user.press(card.getByRole('button', { name: 'Update now' }));
  const sheet = within(await screen.findByTestId('agent-update-sheet'));
  await user.press(await sheet.findByRole('button', { name: 'Update now' }));
  expect(await sheet.findByText('The agent is now 0.5.1.')).toBeTruthy();
});

test('My PCs also asks for the latest release, and shows the card when the agent is behind it', async () => {
  if (config) config.version = '0.5.0';
  await paired();
  await renderRouter(appDir, { initialUrl: '/pcs' });
  expect(await screen.findByText('The agent on this PC is 0.5.0; the new version is 0.5.1.')).toBeTruthy();
  expect(agent.updateCalls).toContain('GET');
});

test('in Korean', async () => {
  setLang('ko');
  await paired();
  const user = userEvent.setup();
  await renderRouter(appDir, { initialUrl: '/settings' });
  await user.press(await screen.findByRole('button', { name: /^DESKTOP-TEST, / }));
  const sheet = within(await screen.findByTestId('pc-sheet'));
  expect(await sheet.findByText('에이전트 0.5.0 판 → 새 판 0.5.1')).toBeTruthy();
  await user.press(sheet.getByRole('button', { name: '지금 업데이트' }));
  expect(await sheet.findByText('에이전트를 0.5.1 판으로 올렸습니다.')).toBeTruthy();
});
