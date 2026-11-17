// The tests' handle on the stand-ins for photos (see setup.ts): whether the
// build can get photos, what the picker and the camera give next, and what
// became of the files.

import type { TurnPhoto } from '@/ask/photos';

type PickState = { available: boolean; picked: TurnPhoto[] | Error; taken: TurnPhoto | null | Error; calls: string[]; n: number };
type FileState = { kept: string[]; dropped: string[]; conversations: string[]; all: number; pending: number };

export const mockPhotos: PickState = (jest.requireMock('@/ask/photoPick') as { mockPhotoState: PickState }).mockPhotoState;
export const mockPhotoFiles: FileState = (jest.requireMock('@/ask/photoFiles') as { mockPhotoFiles: FileState }).mockPhotoFiles;

/** A photo as the native module hands it over: a scaled-down copy waiting in the app's cache. */
export function pendingPhoto(name: string, width = 2048, height = 1536): TurnPhoto {
  return { uri: `file:///data/user/0/app.askorus/cache/photos-pending/${name}.jpg`, width, height };
}

/** Back to a build without photos and with nothing picked or kept. */
export function resetMockPhotos() {
  mockPhotos.available = false;
  mockPhotos.picked = [];
  mockPhotos.taken = null;
  mockPhotos.calls = [];
  mockPhotoFiles.kept = [];
  mockPhotoFiles.dropped = [];
  mockPhotoFiles.conversations = [];
  mockPhotoFiles.all = 0;
  mockPhotoFiles.pending = 0;
}

/** The error the stand-in throws for a camera that is not allowed, a build that cannot, or anything else. */
export function photoError(code: 'denied' | 'unavailable' | 'failed'): Error {
  const { PhotoError } = jest.requireMock('@/ask/photoPick') as { PhotoError: new (code: string) => Error };
  return new PhotoError(code);
}
