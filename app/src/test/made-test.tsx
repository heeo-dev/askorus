// Pictures the AIs make in their answers (#14, made pictures, 0.6.0) against
// a pretend agent: the question says the app shows them, the PC's pictures
// are taken into the conversation's folder as soon as the run names them,
// they show under the AI's answer and open large with Save, one that did
// not arrive shows Try again, the person can turn an AI's pictures off in
// Manage AIs, and the AI's picture limit is said. Every user event is
// awaited (R-07).

import { render, userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { AgentClient, baseUrl } from '@/api/client';
import type { Run } from '@/api/types';
import { conversations } from '@/ask/conversations';
import { madeOf, madeWanted, takeMade } from '@/ask/made';
import type { Turn } from '@/ask/thread';
import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { reportMail } from '@/ask/report';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { mockPhotoFiles, mockPhotoSave } from '@/test/photos';
import { PictureRow, rowPictures, shownTiles } from '@/ui/PictureRow';

type MadeFiles = { targets: string[]; bad: string[] };
const mockMadeFiles = (jest.requireMock('@/ask/madeFiles') as { mockMadeFiles: MadeFiles }).mockMadeFiles;

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

const png = { type: 'image/png', bytes: 1000, width: 1024, height: 1024 };

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableMaking();
  agent.madePictures = { codex: [png] };
  globalThis.fetch = agent.fetch;
  mockMadeFiles.targets = [];
  mockMadeFiles.bad = [];
  mockPhotoSave.saved = [];
  mockPhotoSave.fail = null;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  // Codex alone answers, so its answer shows in full on the screen.
  await saveAskPrefs({ ...defaultAskPrefs, off: ['claude', 'opencode'], ...prefs });
  const r = await renderRouter(appDir, { initialUrl: '/' });
  return { user, r };
}

async function ask(user: User, question: string) {
  await user.type(await screen.findByLabelText('Ask a question'), question);
  await user.press(screen.getByRole('button', { name: 'Ask' }));
}

const madeUri = expect.stringMatching(/^file:\/\/\/data\/user\/0\/app\.askorus\/files\/photos\/[0-9a-f-]{36}\/made-img_[0-9a-f]{32}\.png$/);

test("a picture the AI made is taken into the conversation's folder and shows under its answer, large with Save", async () => {
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  expect(await screen.findByText('codex says: Make a photo of a cat')).toBeTruthy();
  const req = agent.runRequests()[0];
  expect(req.pictures).toBe(true);
  expect(req.noPictures).toBeUndefined();
  const pic = await screen.findByRole('imagebutton', { name: 'Picture the AI made, 1 of 1. Opens it large.' });
  expect(agent.imageRequests).toHaveLength(1);
  expect(mockMadeFiles.targets).toEqual([madeUri]);

  await user.press(pic);
  const viewer = await screen.findByTestId('made-viewer');
  await user.press(within(viewer).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(mockPhotoSave.saved).toEqual([madeUri]));
  expect(within(viewer).getByText("Saved to your phone's photos.")).toBeTruthy();
});

test('a picture that did not arrive says Try again, and arrives on it', async () => {
  agent.imageFails = 1;
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  const again = await screen.findByRole('button', { name: 'Picture 1 of 1 did not arrive. Try again' });
  await user.press(again);
  expect(await screen.findByRole('imagebutton', { name: 'Picture the AI made, 1 of 1. Opens it large.' })).toBeTruthy();
  expect(agent.imageRequests).toHaveLength(2);
});

test("a file that is not the PC's picture is not shown", async () => {
  mockMadeFiles.bad = [];
  const real = agent.fetch;
  globalThis.fetch = async (input, init) => {
    const res = await real(input, init);
    // The check finds the file is not the picture named (its size or first bytes).
    if (String(input).includes('/images/')) mockMadeFiles.bad.push(...mockMadeFiles.targets);
    return res;
  };
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  expect(await screen.findByRole('button', { name: 'Picture 1 of 1 did not arrive. Try again' })).toBeTruthy();
  expect(screen.queryByRole('imagebutton', { name: /Picture the AI made/ })).toBeNull();
});

test('turned off for an AI, the question says so and nothing is taken', async () => {
  const { user } = await open({ noPictures: ['codex'] });
  await ask(user, 'Make a photo of a cat');
  await screen.findByText('codex says: Make a photo of a cat');
  expect(agent.runRequests()[0]).toMatchObject({ pictures: true, noPictures: ['codex'] });
  expect(agent.imageRequests).toEqual([]);
  expect(screen.queryByTestId('made-pictures')).toBeNull();
});

test('a PC whose AIs make no pictures gets no picture fields', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  globalThis.fetch = agent.fetch;
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  await screen.findByText('codex says: Make a photo of a cat');
  expect('pictures' in agent.runRequests()[0]).toBe(false);
  expect('noPictures' in agent.runRequests()[0]).toBe(false);
});

test("the AI's picture limit is said under its answer", async () => {
  agent.madePictures = {};
  agent.madeLimit = { codex: { until: '2026-11-24T16:26:01Z' } };
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  expect(await screen.findByTestId('made-limit')).toHaveTextContent(/^This AI's picture limit is reached, so it made no picture\. It can make pictures again from /);
});

test('Manage AIs has a picture switch for an AI that makes them, with its limit', async () => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enableMaking('2026-11-24T16:26:01Z');
  globalThis.fetch = agent.fetch;
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  await renderRouter(appDir, { initialUrl: '/ais' });
  await user.press(await screen.findByRole('button', { name: /^Codex/ }));
  const sheet = await screen.findByTestId('ai-sheet');
  expect(within(sheet).getByText('Makes pictures')).toBeTruthy();
  expect(within(sheet).getByText(/^Picture limit reached\. It can make pictures again from /)).toBeTruthy();
  const sw = within(sheet).getByRole('switch', { name: 'Make pictures: Codex' });
  expect(sw).toBeChecked();
  await user.press(sw);
  await waitFor(async () => expect((await loadAskPrefs()).noPictures).toEqual(['codex']));
  expect(within(sheet).getByRole('switch', { name: 'Make pictures: Codex' })).not.toBeChecked();
});

test('the made pictures of a turn: what shows, and what is still to take', () => {
  const run = {
    runId: 'run-1',
    mode: 'ask',
    status: 'done',
    startedAt: 'x',
    parts: [
      { agent: 'codex', status: 'done', images: [{ id: `img_${'a'.repeat(32)}`, ...png }, { id: `img_${'b'.repeat(32)}`, ...png }] },
      { agent: 'claude', status: 'done' },
    ],
  } as Run;
  const turn = { id: 't1', run, made: { [`img_${'a'.repeat(32)}`]: 'file:///a.png' }, madeFailed: [`img_${'b'.repeat(32)}`] };
  expect(madeOf(turn, 'codex').map((m) => [m.uri, m.failed])).toEqual([
    ['file:///a.png', undefined],
    [undefined, true],
  ]);
  expect(madeOf(turn, 'claude')).toEqual([]);
  expect(madeWanted(run, turn)).toEqual([]);
  expect(madeWanted(run, {}).map((im) => im.id)).toEqual([`img_${'a'.repeat(32)}`, `img_${'b'.repeat(32)}`]);
});

test('takeMade refuses ids that name no file, and a PC answer that is not a picture', async () => {
  const client = new AgentClient(baseUrl(HOST, PORT), { token: TOKEN });
  expect(await takeMade(client, 'conversation-1', 'run-1', { id: '../../x', type: 'image/png', bytes: 1 })).toBeNull();
  expect(await takeMade(client, 'conversation-1', 'run-1', { id: `img_${'c'.repeat(32)}`, type: 'image/gif', bytes: 1 })).toBeNull();
  // The PC has no such picture (another phone's, or a day old).
  expect(await takeMade(client, 'conversation-1', 'run-1', { id: `img_${'c'.repeat(32)}`, type: 'image/png', bytes: 1 })).toBeNull();
});

test('in the row, the pictures an AI made come first and are no web views', async () => {
  const shown = (id: string, uri?: string) => ({ turnId: 't', runId: 'r', image: { id, ...png }, uri });
  const block = '```svg\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>\n```';
  const answers = [
    { agent: 'codex', name: 'Codex', text: block, made: [shown(`img_${'a'.repeat(32)}`, 'file:///a.png')] },
    { agent: 'claude', name: 'Claude', text: Array.from({ length: 7 }, () => block).join('\n\n') },
  ];
  const pics = rowPictures(answers);
  expect(pics.map((p) => [p.agent, p.n, p.of, !!p.made])).toEqual([
    ['codex', 1, 2, true],
    ['codex', 2, 2, false],
    ['claude', 1, 4, false],
    ['claude', 2, 4, false],
    ['claude', 3, 4, false],
    ['claude', 4, 4, false],
  ]);
  expect(shownTiles(pics)).toBe(6);
  await render(<PictureRow answers={answers} />);
  expect(screen.getByRole('header', { name: 'Pictures by AI (6)' })).toBeTruthy();
  expect(screen.getAllByTestId('picture-tile-webview')).toHaveLength(5);
});

test('a report says how many pictures the answer had, and sends none', () => {
  const mail = reportMail({ ai: 'Codex', text: 'Here.', pictures: 2 }, { category: 'other', withText: false, version: '0.6.0', os: 'android' });
  expect(mail.body).toContain('Pictures it made: 2 (not attached)');
});

// The next picture request waits until the test lets it go.
function holdImages() {
  const real = agent.fetch;
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => {
    release = r;
  });
  globalThis.fetch = async (input, init) => {
    if (String(input).includes('/images/')) await gate;
    return real(input, init);
  };
  return () => release();
}

// R-163 (2호's review of G1-G6, middle): a conversation removed while its
// pictures were being taken got its folder made again by the next picture,
// with pictures in it that no conversation knew.
test('a conversation removed while its pictures come takes no more, and its folder goes', async () => {
  agent.madePictures = { codex: [png, png] };
  const release = holdImages();
  const { user } = await open();
  await ask(user, 'Make two photos of a cat');
  await screen.findByText('codex says: Make two photos of a cat');
  await waitFor(() => expect(mockMadeFiles.targets).toHaveLength(1));
  const id = conversations.current()?.id as string;
  await conversations.remove(id);
  release();
  await waitFor(() => expect(mockPhotoFiles.conversations).toContain(id));
  expect(mockMadeFiles.targets).toHaveLength(1);
});

// R-164 (2호's review, middle): a picture that arrived after its turn had
// left the screen (its conversation ended in the background, or another one
// was opened) was lost, and opening the conversation again never took it.
test('a picture that comes after its conversation left the screen is kept with it', async () => {
  const release = holdImages();
  const { user } = await open();
  await ask(user, 'Make a photo of a cat');
  await screen.findByText('codex says: Make a photo of a cat');
  await waitFor(() => expect(mockMadeFiles.targets).toHaveLength(1));
  const id = conversations.current()?.id as string;
  await user.press(screen.getByRole('button', { name: 'New question' }));
  release();
  await waitFor(async () => {
    const kept = await conversations.load(id);
    expect(Object.values(kept?.turns[0]?.made ?? {})).toEqual([madeUri]);
  });
});

test('opening a conversation takes the pictures it has not taken yet', async () => {
  const { user } = await open();
  const conn = (await new ConnectionStore().list())[0];
  const turn = {
    id: 'turn-old',
    question: 'Make a photo of a cat',
    req: { mode: 'ask', prompt: 'Make a photo of a cat', agents: [{ agent: 'codex' }] },
    agents: ['codex'],
    run: { runId: 'run-old', mode: 'ask', status: 'done', startedAt: 'x', parts: [{ agent: 'codex', status: 'done', answer: 'Here.', images: [{ id: `img_${'d'.repeat(32)}`, ...png }] }] },
  } as unknown as Turn;
  await conversations.save({ id: 'conv-made', pcId: conn.id, startedAt: 1, title: 'Kept with a picture', turns: [turn] });
  await user.press(await screen.findByRole('button', { name: 'Open menu' }));
  await user.press(within(await screen.findByTestId('drawer')).getByRole('button', { name: 'Kept with a picture' }));
  await waitFor(() => expect(agent.imageRequests).toEqual([`run-old/images/img_${'d'.repeat(32)}`]));
  // The pretend PC no longer has it (a day old): it says so, with Try again.
  expect(await screen.findByRole('button', { name: 'Picture 1 of 1 did not arrive. Try again' })).toBeTruthy();
});
