// Where the pictures AIs make live on the phone (#14, made pictures): in the
// conversation's own folder of photos (photoFiles.ts), as made-<id>.<ext>,
// for as long as the conversation is kept; removing the conversation
// removes them with its photos. The bytes are the AI's as the PC copied
// them: nothing is encoded again, so the mark the AI's company put in the
// picture stays (decision 8). A file is shown only after its size and its
// first bytes were checked again here.
//
// Kept apart from the rest so tests can put a stand-in here.

import { Directory, File, Paths } from 'expo-file-system';

import type { MadeImage } from '@/api/types';

const keptRoot = () => new Directory(Paths.document, 'photos');

// A conversation's id is a UUID the app made; a picture's the PC's.
const safeId = (id: string) => /^[0-9a-zA-Z-]{8,64}$/.test(id);
const safeImage = (id: string) => /^img_[0-9a-f]{32}$/.test(id);

/** The ending of each picture type. */
export const madeExt: Record<string, string> = { 'image/png': '.png', 'image/jpeg': '.jpg', 'image/webp': '.webp' };

/** At most this many bytes a picture (the PC sends up to 8 MiB). */
export const maxMadeBytes = 8 << 20;

/**
 * madeTarget is where a picture of a conversation is taken to: a file://
 * address in its folder where nothing is (anything left there by a take cut
 * off half way is deleted first), or null for an id or type that names no
 * file.
 */
export function madeTarget(conversationId: string, image: MadeImage): string | null {
  const ext = madeExt[image.type];
  if (!ext || !safeId(conversationId) || !safeImage(image.id)) return null;
  try {
    const folder = new Directory(keptRoot(), conversationId);
    folder.create({ intermediates: true, idempotent: true });
    const file = new File(folder, `made-${image.id}${ext}`);
    if (file.exists) file.delete();
    return file.uri;
  } catch {
    return null;
  }
}

/** sniff names the picture type of the first bytes, or ''. */
export function sniff(b: Uint8Array): string {
  if (b.length >= 3 && b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff) return 'image/jpeg';
  if (b.length >= 8 && [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a].every((v, i) => b[i] === v)) return 'image/png';
  const text = (from: number, to: number) => String.fromCharCode(...b.slice(from, to));
  if (b.length >= 12 && text(0, 4) === 'RIFF' && text(8, 12) === 'WEBP') return 'image/webp';
  return '';
}

/**
 * checkMade reports whether the file at uri is the picture the PC named:
 * its size and its type by its first bytes. A file that is not is deleted.
 */
export function checkMade(uri: string, image: MadeImage): boolean {
  let ok = false;
  try {
    const file = new File(uri);
    const handle = file.open();
    try {
      const size = handle.size ?? -1;
      ok = size > 0 && size <= maxMadeBytes && size === image.bytes && sniff(handle.readBytesSync(16)) === image.type;
    } finally {
      handle.close();
    }
    if (!ok && file.exists) file.delete();
  } catch {
    ok = false;
  }
  return ok;
}
