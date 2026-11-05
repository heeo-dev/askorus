// Jest setup for screen tests: native modules the screens touch are
// replaced with small in-memory stand-ins. Each test file still mocks
// `fetch` itself (see fakeAgent.ts). The app's in-memory stores (S5c: the
// conversations, the paired PCs and their status) start empty in every
// test, as the secure store and the history store (milestone B) do.

import { conversations } from '@/ask/conversations';
import { setLang } from '@/i18n';
import { resetPCs } from '@/ui/shell/pcs';
import { resetPCStatus } from '@/ui/shell/pcStatus';

const mockSecure = new Map<string, string>();
const mockSQLite = new Map<string, Map<string, string>>();

// The history's SQLite key-value store (history.ts): a map per database.
jest.mock('expo-sqlite/kv-store', () => ({
  SQLiteStorage: class {
    private m: Map<string, string>;
    constructor(name: string) {
      if (!mockSQLite.has(name)) mockSQLite.set(name, new Map());
      this.m = mockSQLite.get(name)!;
    }
    getItemAsync = async (k: string) => this.m.get(k) ?? null;
    setItemAsync = async (k: string, v: string) => void this.m.set(k, v);
    removeItemAsync = async (k: string) => this.m.delete(k);
    getAllKeysAsync = async () => [...this.m.keys()];
  },
}));

jest.mock('expo-secure-store', () => ({
  getItemAsync: async (k: string) => mockSecure.get(k) ?? null,
  setItemAsync: async (k: string, v: string) => void mockSecure.set(k, v),
  deleteItemAsync: async (k: string) => void mockSecure.delete(k),
}));

jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID() }));

jest.mock('expo-device', () => ({ deviceName: 'Test Phone', modelName: 'SM-TEST' }));

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn(async () => true), getStringAsync: jest.fn(async () => '') }));

// The theme sets the window's background (S5c); tests can read what it set.
jest.mock('expo-system-ui', () => ({ setBackgroundColorAsync: jest.fn(async () => undefined), getBackgroundColorAsync: jest.fn(async () => null) }));

jest.mock('expo-camera', () => ({
  CameraView: () => null,
  useCameraPermissions: () => [{ granted: false, canAskAgain: true }, jest.fn()],
}));

// Display math (MathBlock) draws in a WebView: a plain View stands in and
// keeps its props, so tests can read source.html and call onMessage.
jest.mock('react-native-webview', () => {
  const { createElement } = require('react');
  const { View } = require('react-native');
  const WebView = (props: Record<string, unknown>) => createElement(View, { ...props, testID: 'math-webview' });
  return { __esModule: true, default: WebView, WebView };
});

setLang('en');

beforeEach(() => {
  mockSecure.clear();
  for (const m of mockSQLite.values()) m.clear();
  conversations.reset();
  resetPCs();
  resetPCStatus();
});
