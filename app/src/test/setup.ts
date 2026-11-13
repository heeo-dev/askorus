// Jest setup for screen tests: native modules the screens touch are
// replaced with small in-memory stand-ins. Each test file still mocks
// `fetch` itself (see fakeAgent.ts). The app's in-memory stores (S5c: the
// conversations, the paired PCs and their status) start empty in every
// test, as the secure store and the history store (milestone B) do.

import { conversations } from '@/ask/conversations';
import { setLang } from '@/i18n';
import { resetTailnet } from '@/tailnet/node';
import { resetMockTailnet } from './tailnet';
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

jest.mock('expo-crypto', () => ({
  randomUUID: () => require('node:crypto').randomUUID(),
  getRandomBytes: (n: number) => new Uint8Array(require('node:crypto').randomBytes(n)),
}));

jest.mock('expo-device', () => ({ deviceName: 'Test Phone', modelName: 'SM-TEST' }));

// The app's own Tailscale node (#57, modules/tailnet): a stand-in that is
// signed in and hands each request to the test's own `fetch` (fakeAgent).
// Every PC name is one of the person's own devices unless a test says
// otherwise through mockTailnet (test/tailnet.ts).
jest.mock('@/tailnet/native', () => {
  const state = {
    available: true,
    status: { state: 'Running', name: 'askorus-test-phone.tail0.ts.net', owner: 'me@example.com' } as Record<string, unknown>,
    // What the tailnet says of a host; the default is one of the person's own devices.
    peers: {} as Record<string, { known: boolean; sameUser: boolean }>,
    // What login makes of the status (the link arrives a moment after the button).
    afterLogin: null as Record<string, unknown> | null,
    calls: [] as string[],
  };
  const mod = {
    start: async (hostname: string) => void state.calls.push(`start ${hostname}`),
    stop: async () => void state.calls.push('stop'),
    status: async () => JSON.stringify(state.status),
    login: async () => {
      state.calls.push('login');
      if (state.afterLogin) state.status = state.afterLogin;
    },
    logout: async () => {
      state.calls.push('logout');
      state.status = { state: 'NeedsLogin' };
    },
    peer: async (host: string) => {
      state.calls.push(`peer ${host}`);
      return JSON.stringify(state.peers[host] ?? { known: true, sameUser: true, online: true });
    },
    fetch: async (method: string, url: string, headers: string, body: string) => {
      const res = await globalThis.fetch(url, { method, headers: JSON.parse(headers), body: body || undefined });
      return { status: res.status, headers: '{}', body: await res.text() };
    },
    recentLog: async () => '',
  };
  return { __esModule: true, nativeTailnet: () => (state.available ? mod : null), mockTailnetState: state };
});

jest.mock('expo-clipboard', () => ({ setStringAsync: jest.fn(async () => true), getStringAsync: jest.fn(async () => '') }));

// The theme sets the window's background (S5c); tests can read what it set.
jest.mock('expo-system-ui', () => ({ setBackgroundColorAsync: jest.fn(async () => undefined), getBackgroundColorAsync: jest.fn(async () => null) }));

// The app's QR scanner (a native view): a plain View stands in and keeps its
// props, so a test can hand it a scanned text with onScanned. The camera is
// not allowed yet unless a test says so.
jest.mock('@/ui/QrScanner', () => {
  const { createElement } = require('react');
  const { View } = require('react-native');
  return {
    __esModule: true,
    scannerAvailable: () => true,
    useCameraPermission: () => [{ granted: false, canAskAgain: true }, async () => undefined],
    QrScanner: (props: Record<string, unknown>) => createElement(View, { ...props, testID: 'qr-scanner' }),
  };
});

// Display math (MathBlock) draws in a WebView: a plain View stands in and
// keeps its props, so tests can read source.html and call onMessage.
jest.mock('react-native-webview', () => {
  const { createElement } = require('react');
  const { View } = require('react-native');
  const WebView = (props: Record<string, unknown>) => createElement(View, { ...props, testID: 'math-webview' });
  return { __esModule: true, default: WebView, WebView };
});

// Screens are tested still: the phone 'asks for less motion', so the live
// motions (ui/motion.tsx) show their final look and run no loops.
jest.spyOn(require('react-native').AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(true);

setLang('en');

beforeEach(() => {
  mockSecure.clear();
  // The person signed in to Tailscale on this phone before (tailnet/node):
  // the screens under test are those of a phone that can reach its PCs.
  mockSecure.set('tailnet.on.v1', '1');
  resetMockTailnet();
  resetTailnet();
  for (const m of mockSQLite.values()) m.clear();
  conversations.reset();
  resetPCs();
  resetPCStatus();
});
