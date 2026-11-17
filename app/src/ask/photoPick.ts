// Getting a photo for a question (#14): the app's own native module
// (modules/photo-picker) opens Android's photo picker or the phone's camera
// app and hands back a scaled-down JPEG copy in the app's cache. The copy
// carries the pixels only: where and when the photo was taken stay behind.
// Kept apart from the rest so tests can put a stand-in here.
//
// Android only for now. Where the module is missing (Expo Go, iOS until its
// build, tests) photosAvailable() is false and the + button is not shown.

import { requireOptionalNativeModule } from 'expo';
import { PermissionsAndroid, Platform } from 'react-native';

import { photoEdge, photoQuality, type TurnPhoto } from './photos';

type NativePhoto = { uri: string; width: number; height: number; bytes: number };

export type NativePhotoPicker = {
  /** The photos the person picked (at most limit), scaled down; [] when the picker was closed. */
  pick(limit: number, edge: number, quality: number): Promise<NativePhoto[]>;
  /** One photo from the camera app, scaled down; null when none was taken. */
  capture(edge: number, quality: number): Promise<NativePhoto | null>;
};

let cached: NativePhotoPicker | null | undefined;

/** The module, or null in a build without it. */
export function nativePhotoPicker(): NativePhotoPicker | null {
  if (cached === undefined) cached = Platform.OS === 'android' ? requireOptionalNativeModule<NativePhotoPicker>('AskorusPhotoPicker') : null;
  return cached && typeof cached.pick === 'function' ? cached : null;
}

/** Whether this build can add photos to a question at all. */
export function photosAvailable(): boolean {
  return nativePhotoPicker() !== null;
}

/**
 * Why a photo could not be got: 'denied' the camera is not allowed (and
 * Android will not ask again: only the phone's Settings can allow it),
 * 'unavailable' this build or phone cannot (no module, no camera app, no
 * picker), 'failed' anything else.
 */
export class PhotoError extends Error {
  constructor(readonly code: 'denied' | 'unavailable' | 'failed') {
    super(code);
    this.name = 'PhotoError';
  }
}

const quality = Math.round(photoQuality * 100);

function photo(p: NativePhoto): TurnPhoto | null {
  if (!p || typeof p.uri !== 'string' || !p.uri.startsWith('file://') || !(p.width > 0) || !(p.height > 0)) return null;
  return { uri: p.uri, width: Math.round(p.width), height: Math.round(p.height) };
}

function fail(e: unknown): PhotoError {
  const code = (e as { code?: string } | null)?.code;
  if (code === 'photo_camera_denied') return new PhotoError('denied');
  if (code === 'photo_no_camera' || code === 'photo_no_picker' || code === 'photo_not_ready') return new PhotoError('unavailable');
  return new PhotoError('failed');
}

/** pickPhotos opens the phone's photo picker for up to limit photos. [] when the person picked none. */
export async function pickPhotos(limit: number): Promise<TurnPhoto[]> {
  const native = nativePhotoPicker();
  if (!native) throw new PhotoError('unavailable');
  if (limit < 1) return [];
  try {
    const got = await native.pick(Math.floor(limit), photoEdge, quality);
    return (Array.isArray(got) ? got : []).map(photo).filter((p): p is TurnPhoto => p !== null).slice(0, limit);
  } catch (e) {
    throw fail(e);
  }
}

/**
 * takePhoto opens the phone's camera app for one photo; null when none was
 * taken. Android wants the camera permission first, because the app has it
 * in its manifest (for the pairing QR code): it is asked for here.
 */
export async function takePhoto(): Promise<TurnPhoto | null> {
  const native = nativePhotoPicker();
  if (!native) throw new PhotoError('unavailable');
  const camera = PermissionsAndroid.PERMISSIONS.CAMERA;
  const allowed = (await PermissionsAndroid.check(camera).catch(() => false)) || (await PermissionsAndroid.request(camera).catch(() => 'denied')) === PermissionsAndroid.RESULTS.GRANTED;
  if (!allowed) throw new PhotoError('denied');
  try {
    const got = await native.capture(photoEdge, quality);
    return got ? photo(got) : null;
  } catch (e) {
    throw fail(e);
  }
}
