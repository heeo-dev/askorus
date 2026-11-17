// Saving a picture into the phone's own pictures (#14): the PNG of a
// drawing an AI made as code (SvgBlock). The app's photo module puts it in
// Pictures/Askorus through Android's MediaStore, which needs no permission
// on Android 10 and later; older phones cannot (the app asks for no storage
// permission). Kept apart so tests can put a stand-in here.

import { File, Paths } from 'expo-file-system';

import { nativePhotoPicker } from './photoPick';

/** Why a picture was not saved: this phone or build cannot, or it failed. */
export class SaveError extends Error {
  constructor(readonly code: 'unsupported' | 'failed') {
    super(code);
    this.name = 'SaveError';
  }
}

/** Whether this build can save pictures at all. */
export function savingAvailable(): boolean {
  const native = nativePhotoPicker();
  return !!native && typeof native.savePicture === 'function';
}

const two = (n: number) => String(n).padStart(2, '0');

/** pictureName is the saved file's name: askorus-YYYYMMDD-HHMMSS.png, by the phone's clock. */
export function pictureName(now = new Date()): string {
  return `askorus-${now.getFullYear()}${two(now.getMonth() + 1)}${two(now.getDate())}-${two(now.getHours())}${two(now.getMinutes())}${two(now.getSeconds())}.png`;
}

/** savePicture puts a PNG (its bytes as base64) into the phone's pictures. */
export async function savePicture(pngBase64: string): Promise<void> {
  const native = nativePhotoPicker();
  if (!native || typeof native.savePicture !== 'function') throw new SaveError('unsupported');
  const name = pictureName();
  const file = new File(Paths.cache, name);
  try {
    if (!file.exists) file.create();
    await file.write(pngBase64, { encoding: 'base64' });
    await native.savePicture(file.uri, name);
  } catch (e) {
    throw new SaveError((e as { code?: string } | null)?.code === 'photo_save_unsupported' ? 'unsupported' : 'failed');
  } finally {
    try {
      if (file.exists) file.delete();
    } catch {
      // The cache is the phone's to clear.
    }
  }
}
