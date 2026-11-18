// Where the photos of questions live on the phone (#14). A photo is first a
// file in the app's cache (photos-pending, written by the native module);
// when its question goes it moves into the app's own files, in a folder per
// conversation, and stays there for as long as the conversation is kept.
// Removing a conversation removes its folder. Nothing here leaves the
// phone, and the app is kept out of Android's backup.
//
// Kept apart from the rest so tests can put a stand-in here.

import { Directory, File, Paths } from 'expo-file-system';

import type { TurnPhoto } from './photos';

const keptRoot = () => new Directory(Paths.document, 'photos');
const pendingRoot = () => new Directory(Paths.cache, 'photos-pending');
// Where the camera app writes a new picture (the native module deletes it
// once it has its own copy; one is left only when the app died in between).
const captureRoot = () => new Directory(Paths.cache, 'photo-capture');

// A conversation's id is a UUID the app made; anything else names no folder.
const safeId = (id: string) => /^[0-9a-zA-Z-]{8,64}$/.test(id);

function quiet(job: () => void) {
  try {
    job();
  } catch {
    // A file that is already gone, or cannot be deleted now, is left.
  }
}

/**
 * keepPhotos moves a question's photos into its conversation's folder and
 * gives them back with their new addresses. A photo that is already kept,
 * or cannot be moved, is given back as it is.
 */
export async function keepPhotos(conversationId: string, photos: TurnPhoto[]): Promise<TurnPhoto[]> {
  if (!photos.length || !safeId(conversationId)) return photos;
  const folder = new Directory(keptRoot(), conversationId);
  try {
    folder.create({ intermediates: true, idempotent: true });
  } catch {
    return photos;
  }
  const out: TurnPhoto[] = [];
  for (const p of photos) {
    try {
      const from = new File(p.uri);
      if (from.parentDirectory.uri === folder.uri) {
        out.push(p);
        continue;
      }
      const to = new File(folder, from.name);
      await from.move(to);
      out.push({ ...p, uri: to.uri });
    } catch {
      out.push(p);
    }
  }
  return out;
}

/** dropPhotos deletes photos that were taken off a question before it went. */
export function dropPhotos(photos: TurnPhoto[]): void {
  for (const p of photos) quiet(() => new File(p.uri).delete());
}

/** dropConversationPhotos deletes the photos of one conversation. */
export function dropConversationPhotos(conversationId: string): void {
  if (!safeId(conversationId)) return;
  quiet(() => {
    const folder = new Directory(keptRoot(), conversationId);
    if (folder.exists) folder.delete();
  });
}

/** dropAllPhotos deletes every kept photo, and those still waiting. */
export function dropAllPhotos(): void {
  for (const root of [keptRoot, pendingRoot, captureRoot]) {
    quiet(() => {
      const folder = root();
      if (folder.exists) folder.delete();
    });
  }
}

/** dropPending deletes the photos that were picked and never sent (the app was closed first), and a camera picture left half way. */
export function dropPending(): void {
  for (const root of [pendingRoot, captureRoot]) {
    quiet(() => {
      const folder = root();
      if (folder.exists) folder.delete();
    });
  }
}
