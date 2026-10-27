// Jest setup for screen tests: native modules the screens touch are
// replaced with small in-memory stand-ins. Each test file still mocks
// `fetch` itself (see fakeAgent.ts).

import { setLang } from '@/i18n';

const mockSecure = new Map<string, string>();

jest.mock('expo-secure-store', () => ({
  getItemAsync: async (k: string) => mockSecure.get(k) ?? null,
  setItemAsync: async (k: string, v: string) => void mockSecure.set(k, v),
  deleteItemAsync: async (k: string) => void mockSecure.delete(k),
}));

jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID() }));

jest.mock('expo-device', () => ({ deviceName: 'Test Phone', modelName: 'SM-TEST' }));

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn(async () => true) }));

jest.mock('expo-camera', () => ({
  CameraView: () => null,
  useCameraPermissions: () => [{ granted: false, canAskAgain: true }, jest.fn()],
}));

setLang('en');

beforeEach(() => mockSecure.clear());
