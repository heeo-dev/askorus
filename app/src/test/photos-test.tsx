// Photos with a question (#14) on the 한 번에 screen against a pretend agent:
// the + button and its sheet, the photos above the question and beside it
// once sent, what goes to the PC (each photo first, then the question that
// names them), the AIs that sit out because they do not see photos, an
// older PC, a camera that is not allowed, a PC that let the photos go, and
// what becomes of the files. Every user event is awaited (R-07).

import { userEvent, within } from '@testing-library/react-native';
import { renderRouter, screen, waitFor } from 'expo-router/testing-library';

import { conversations } from '@/ask/conversations';
import { defaultAskPrefs, saveAskPrefs, type AskPrefs } from '@/ask/prefs';
import { ConnectionStore } from '@/connections/store';
import { setLang } from '@/i18n';
import { FakeAgent, HOST, PORT, TOKEN } from '@/test/fakeAgent';
import { mockPhotoFiles, mockPhotos, pendingPhoto, photoError } from '@/test/photos';

let agent: FakeAgent;
const appDir = './src/app';
type User = ReturnType<typeof userEvent.setup>;

beforeEach(() => {
  agent = new FakeAgent();
  agent.enableAsk();
  agent.enablePhotos();
  globalThis.fetch = agent.fetch;
  mockPhotos.available = true;
});

afterEach(() => {
  setLang('en');
  jest.restoreAllMocks();
});

async function open(prefs: Partial<AskPrefs> = {}) {
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  if (Object.keys(prefs).length) await saveAskPrefs({ ...defaultAskPrefs, ...prefs });
  const r = await renderRouter(appDir, { initialUrl: '/' });
  await screen.findByLabelText('Ask a question');
  return { user, r };
}

async function pick(user: User, names: string[]) {
  mockPhotos.picked = names.map((n) => pendingPhoto(n));
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  await user.press(await screen.findByRole('button', { name: 'Choose photos' }));
}

const strip = () => within(screen.getByTestId('composer-photos'));
const kept = (name: string) => expect.stringMatching(new RegExp(`^file:///data/user/0/app\\.askorus/files/photos/[0-9a-f-]{36}/${name}\\.jpg$`));

test('photos picked for a question go to the PC first, then the question that names them', async () => {
  const { user } = await open();
  await pick(user, ['a', 'b']);
  expect(mockPhotos.calls).toEqual(['pick 4']);
  expect(await strip().findByRole('imagebutton', { name: 'Open photo 1 of 2' })).toBeTruthy();
  expect(strip().getByRole('button', { name: 'Remove photo 2' })).toBeTruthy();
  // Nothing is sent while the question is being written.
  expect(agent.uploaded).toEqual([]);

  await user.type(screen.getByLabelText('Ask a question'), 'What is this sign?');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('opencode says: What is this sign?');

  // The photos moved from the cache into the conversation's own folder, and those files went.
  expect(agent.uploaded).toEqual([kept('a'), kept('b')]);
  expect(mockPhotoFiles.kept).toEqual([kept('a'), kept('b')]);
  const req = agent.runRequests()[0];
  expect(req.attachments).toEqual([...agent.uploads.keys()]);
  expect(req.attachments).toHaveLength(2);
  expect(req.prompt).toBe('What is this sign?');
  expect(req.agents.map((a: { agent: string }) => a.agent)).toEqual(['claude', 'codex', 'opencode']);
  // Each upload carried the device token and no question.
  for (const c of agent.callsTo('POST', '/v1/uploads')) expect(c.auth).toBe(`Bearer ${TOKEN}`);

  // The question shows its photos; the bar is empty again.
  expect(screen.getByLabelText('A question with 2 photos')).toBeTruthy();
  const sent = within(screen.getByTestId('turn-photos'));
  expect(sent.getByRole('imagebutton', { name: 'Open photo 2 of 2' })).toBeTruthy();
  expect(sent.queryByRole('button', { name: /^Remove photo/ })).toBeNull();
  expect(screen.queryByTestId('composer-photos')).toBeNull();

  // The PC's ids are not kept with the conversation: only the phone's own files are.
  await waitFor(async () => {
    const id = conversations.current()?.id;
    const c = id ? await conversations.load(id) : undefined;
    expect(c?.turns[0].photos?.map((p) => p.uri)).toEqual([kept('a'), kept('b')]);
    expect(JSON.stringify(c)).not.toContain('up_');
  });
});

test('a photo opens large, and its × takes it off the question', async () => {
  const { user } = await open();
  await pick(user, ['a', 'b', 'c']);
  await user.press(await strip().findByRole('imagebutton', { name: 'Open photo 2 of 3' }));
  const viewer = within(await screen.findByTestId('photo-viewer'));
  expect(viewer.getByLabelText('Photo 2 of 3')).toBeTruthy();
  await user.press(viewer.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByTestId('photo-viewer')).toBeNull());

  await user.press(strip().getByRole('button', { name: 'Remove photo 2' }));
  await waitFor(() => expect(strip().queryByRole('imagebutton', { name: 'Open photo 3 of 3' })).toBeNull());
  expect(strip().getByRole('imagebutton', { name: 'Open photo 2 of 2' })).toBeTruthy();
  // The file that was taken off is deleted; nothing was sent to the PC for it.
  expect(mockPhotoFiles.dropped).toEqual([pendingPhoto('b').uri]);
  expect(agent.uploaded).toEqual([]);
});

test('a question takes four photos: the picker is asked for what fits, and the + says so when full', async () => {
  const { user } = await open();
  await pick(user, ['a', 'b', 'c']);
  await strip().findByRole('imagebutton', { name: 'Open photo 3 of 3' });
  // One more fits: the picker is opened for one, and what it gives beyond that is dropped.
  await pick(user, ['d', 'e']);
  await strip().findByRole('imagebutton', { name: 'Open photo 4 of 4' });
  expect(mockPhotos.calls).toEqual(['pick 4', 'pick 1']);
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  expect(await screen.findByText('You can attach up to 4 photos per question.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Choose photos' })).toBeNull();
});

test('the camera gives one photo; a camera that is not allowed says where to allow it', async () => {
  const { user } = await open();
  mockPhotos.taken = pendingPhoto('shot', 1536, 2048);
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  await user.press(await screen.findByRole('button', { name: 'Take photo' }));
  expect(await strip().findByRole('imagebutton', { name: 'Open photo 1 of 1' })).toBeTruthy();

  mockPhotos.taken = photoError('denied');
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  await user.press(await screen.findByRole('button', { name: 'Take photo' }));
  expect(await screen.findByText("The camera isn't allowed. Allow the camera for Askorus in your phone's Settings.")).toBeTruthy();
  // The photo that was there stays.
  expect(strip().getByRole('imagebutton', { name: 'Open photo 1 of 1' })).toBeTruthy();

  // Closing the camera without a picture adds nothing, and the last try's words are gone.
  mockPhotos.taken = null;
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  await user.press(await screen.findByRole('button', { name: 'Take photo' }));
  await waitFor(() => expect(screen.queryByText(/camera isn't allowed/)).toBeNull());
  expect(strip().getByRole('imagebutton', { name: 'Open photo 1 of 1' })).toBeTruthy();
  expect(mockPhotos.calls).toEqual(['take', 'take', 'take']);
});

test('an AI whose model does not see photos sits the question out, and is named before it goes', async () => {
  const { user } = await open({ tier: 'deep' });
  // No photo yet: nobody sits out.
  expect(screen.queryByText(/sit out this question/)).toBeNull();
  await pick(user, ['a']);
  expect(await screen.findByText("AIs that can't see photos sit out this question: OpenCode")).toBeTruthy();
  await user.type(screen.getByLabelText('Ask a question'), 'q');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('codex says: q');
  const req = agent.runRequests()[0];
  expect(req.agents.map((a: { agent: string }) => a.agent)).toEqual(['claude', 'codex']);
  expect(req.attachments).toHaveLength(1);
  expect(screen.queryByText('opencode says: q')).toBeNull();
});

test('when no picked AI sees photos the question cannot go until that changes', async () => {
  const { user } = await open({ tier: 'deep', off: ['claude', 'codex'] });
  await pick(user, ['a']);
  expect(await screen.findByText('None of the selected AIs can see photos. Change the AIs or the tier.')).toBeTruthy();
  await user.type(screen.getByLabelText('Ask a question'), 'q');
  expect(screen.getByRole('button', { name: 'Ask' })).toBeDisabled();
  // Without the photo it goes.
  await user.press(strip().getByRole('button', { name: 'Remove photo 1' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Ask' })).toBeEnabled());
  expect(screen.queryByText(/can see photos/)).toBeNull();
});

test('a PC that takes no photos yet has a dimmed + that says to update it', async () => {
  agent = new FakeAgent();
  agent.enableAsk(); // no enablePhotos: an agent from before photos
  globalThis.fetch = agent.fetch;
  const { user } = await open();
  await user.press(screen.getByRole('button', { name: 'Add photos' }));
  expect(await screen.findByText('Update Askorus on your PC to attach photos.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Choose photos' })).toBeNull();
  expect(mockPhotos.calls).toEqual([]);
});

test('a build without a photo picker shows no +', async () => {
  mockPhotos.available = false;
  await open();
  expect(screen.queryByRole('button', { name: 'Add photos' })).toBeNull();
});

test('a PC that let the photos go gets them again, and the question is asked once', async () => {
  const { user } = await open();
  await pick(user, ['a']);
  await strip().findByRole('imagebutton', { name: 'Open photo 1 of 1' });
  agent.forgetUploads = 1;
  await user.type(screen.getByLabelText('Ask a question'), 'q');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('opencode says: q');
  // Uploaded, refused with upload_gone, uploaded again, sent again.
  expect(agent.uploaded).toHaveLength(2);
  expect(agent.callsTo('POST', '/v1/runs')).toHaveLength(2);
  expect(agent.runRequests()[1].clientRunId).toBe(agent.runRequests()[0].clientRunId);
  expect(agent.runRequests()[1].attachments).not.toEqual(agent.runRequests()[0].attachments);
});

test('a photo the PC refuses leaves the question unsent with the reason, and it can be sent again', async () => {
  const { user } = await open();
  await pick(user, ['a']);
  await strip().findByRole('imagebutton', { name: 'Open photo 1 of 1' });
  agent.uploadError = { status: 429, code: 'upload_limit' };
  await user.type(screen.getByLabelText('Ask a question'), 'q');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  expect(await screen.findByText('Too many photos were sent to the PC. Try again in a few minutes.')).toBeTruthy();
  expect(agent.callsTo('POST', '/v1/runs')).toHaveLength(0);
  // The question keeps its photo, so asking again sends it again.
  agent.uploadError = null;
  await user.press(screen.getByRole('button', { name: 'Try again' }));
  await screen.findByText('opencode says: q');
  expect(agent.runRequests()[0].attachments).toHaveLength(1);
});

test('a follow-up may carry photos too, and one without photos carries none', async () => {
  const { user } = await open();
  await user.type(screen.getByLabelText('Ask a question'), 'first');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('opencode says: first');
  expect(agent.runRequests()[0].attachments).toBeUndefined();

  await pick(user, ['a']);
  await strip().findByRole('imagebutton', { name: 'Open photo 1 of 1' });
  await user.type(screen.getByLabelText('Follow up'), 'and this?');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await screen.findByText('opencode says: and this?');
  expect(agent.runRequests()[1].attachments).toHaveLength(1);
  // One photo is "1 photo", not "1 photos" (seen on a real phone).
  expect(screen.getByLabelText('A question with 1 photo')).toBeTruthy();
  // Both photos live in the one conversation's folder.
  expect(mockPhotoFiles.kept).toHaveLength(1);

  await user.type(screen.getByLabelText('Follow up'), 'thanks');
  await user.press(screen.getByRole('button', { name: 'Send' }));
  await screen.findByText('opencode says: thanks');
  expect(agent.runRequests()[2].attachments).toBeUndefined();
  expect(agent.uploaded).toHaveLength(1);
});

test('a new question drops photos that were picked and never sent', async () => {
  const { user } = await open();
  await user.type(screen.getByLabelText('Ask a question'), 'first');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('opencode says: first');
  await pick(user, ['a', 'b']);
  await strip().findByRole('imagebutton', { name: 'Open photo 2 of 2' });
  await user.press(screen.getByRole('button', { name: 'New question' }));
  await screen.findByLabelText('Ask a question');
  expect(screen.queryByTestId('composer-photos')).toBeNull();
  expect(mockPhotoFiles.dropped).toEqual([pendingPhoto('a').uri, pendingPhoto('b').uri]);
  expect(agent.uploaded).toEqual([]);
});

test('removing a conversation removes its photos; opening the app clears what was never sent', async () => {
  const { user } = await open();
  expect(mockPhotoFiles.pending).toBe(1);
  await pick(user, ['a']);
  await strip().findByRole('imagebutton', { name: 'Open photo 1 of 1' });
  await user.type(screen.getByLabelText('Ask a question'), 'q');
  await user.press(screen.getByRole('button', { name: 'Ask' }));
  await screen.findByText('opencode says: q');
  const id = conversations.current()!.id;
  await waitFor(async () => expect(await conversations.load(id)).toBeTruthy());
  await conversations.remove(id);
  expect(mockPhotoFiles.conversations).toEqual([id]);
  await conversations.removeAll();
  expect(mockPhotoFiles.all).toBe(1);
});

test('the words are Korean in Korean', async () => {
  setLang('ko');
  const user = userEvent.setup();
  await new ConnectionStore().add({ kind: 'pc', name: 'DESKTOP-TEST', host: HOST, port: PORT, deviceId: 'dev-1', deviceName: 'Test Phone', pairedAt: 'x' }, TOKEN);
  await saveAskPrefs({ ...defaultAskPrefs, tier: 'deep' });
  await renderRouter(appDir, { initialUrl: '/' });
  mockPhotos.picked = [pendingPhoto('a')];
  await user.press(await screen.findByRole('button', { name: '사진 붙이기' }));
  expect(await screen.findByRole('button', { name: '사진 찍기' })).toBeTruthy();
  await user.press(screen.getByRole('button', { name: '사진 고르기' }));
  expect(await screen.findByText('사진을 보지 못하는 AI는 이 질문에서 빠집니다: OpenCode')).toBeTruthy();
  expect(screen.getByRole('button', { name: '사진 1 빼기' })).toBeTruthy();
});
