// Jest setup for screen tests: native modules the screens touch are
// replaced with small in-memory stand-ins. Each test file still mocks
// `fetch` itself (see fakeAgent.ts). The app's in-memory stores (S5c: the
// conversations, the paired PCs and their status) start empty in every
// test, as the secure store and the history store (milestone B) do.

import { conversations } from '@/ask/conversations';
import { setLang } from '@/i18n';
import { resetAgentUpdate } from '@/api/agentUpdate';
import { resetRefused } from '@/api/refused';
import { resetTailnet } from '@/tailnet/node';
import { resetMockPhotos } from './photos';
import { resetMockTailnet } from './tailnet';
import { resetPCs } from '@/ui/shell/pcs';
import { resetPCStatus } from '@/ui/shell/pcStatus';
import { resetPCUsage } from '@/ui/shell/pcUsage';

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
    // The other devices of the account, as the node lists them (#58); none unless a test puts some.
    devices: [] as Array<{ name: string; host: string; online: boolean; os?: string }>,
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
    peers: async () => JSON.stringify(state.devices),
    fetch: async (method: string, url: string, headers: string, body: string) => {
      const res = await globalThis.fetch(url, { method, headers: JSON.parse(headers), body: body || undefined });
      return { status: res.status, headers: '{}', body: await res.text() };
    },
    // A photo (#14): the pretend PC gets the file's address in place of its bytes.
    sendFile: async (method: string, url: string, headers: string, path: string) => {
      state.calls.push(`sendFile ${path}`);
      const res = await globalThis.fetch(url, { method, headers: JSON.parse(headers), body: JSON.stringify({ file: path }) });
      return { status: res.status, headers: '{}', body: await res.text() };
    },
    // A made picture (#14): the pretend PC's answer, and the address it went to.
    fetchFile: async (method: string, url: string, headers: string, path: string) => {
      state.calls.push(`fetchFile ${path}`);
      const res = await globalThis.fetch(url, { method, headers: JSON.parse(headers) });
      return { status: res.status, headers: '{}', body: res.ok ? '' : await res.text() };
    },
    recentLog: async () => '',
  };
  return { __esModule: true, nativeTailnet: () => (state.available ? mod : null), mockTailnetState: state };
});

// Photos for a question (#14, modules/photo-picker and the app's files):
// stand-ins a test steers through mockPhotos (test/photos.ts). Until a test
// says otherwise the build has no photo picker, so no screen shows the +.
jest.mock('@/ask/photoPick', () => {
  const state = {
    available: false,
    // What the picker and the camera give next; an Error is thrown.
    picked: [] as unknown,
    taken: null as unknown,
    calls: [] as string[],
    n: 0,
  };
  // Written out (no parameter property): a mock's factory may name nothing outside itself.
  class PhotoError extends Error {
    code: string;
    constructor(why: string) {
      super(why);
      this.name = 'PhotoError';
      this.code = why;
    }
  }
  const give = (v: unknown) => {
    if (v instanceof Error) throw v;
    return v;
  };
  return {
    __esModule: true,
    PhotoError,
    photosAvailable: () => state.available,
    pickPhotos: async (limit: number) => {
      state.calls.push(`pick ${limit}`);
      return (give(state.picked) as unknown[]).slice(0, limit);
    },
    takePhoto: async () => {
      state.calls.push('take');
      return give(state.taken);
    },
    mockPhotoState: state,
  };
});

jest.mock('@/ask/photoFiles', () => {
  const state = { kept: [] as string[], dropped: [] as string[], conversations: [] as string[], all: 0, pending: 0 };
  return {
    __esModule: true,
    // A kept photo's address says which conversation's folder it went to.
    keepPhotos: async (id: string, photos: Array<{ uri: string }>) =>
      photos.map((p) => {
        const uri = p.uri.replace('/cache/photos-pending/', `/files/photos/${id}/`);
        state.kept.push(uri);
        return { ...p, uri };
      }),
    dropPhotos: (photos: Array<{ uri: string }>) => void state.dropped.push(...photos.map((p) => p.uri)),
    dropConversationPhotos: (id: string) => void state.conversations.push(id),
    dropAllPhotos: () => void state.all++,
    dropPending: () => void state.pending++,
    mockPhotoFiles: state,
  };
});

// The pictures AIs make (#14): kept in the conversation's folder by name; a
// test lists which were taken and makes the next check fail.
jest.mock('@/ask/madeFiles', () => {
  const state = { targets: [] as string[], bad: [] as string[] };
  return {
    __esModule: true,
    madeExt: { 'image/png': '.png', 'image/jpeg': '.jpg', 'image/webp': '.webp' },
    madeTarget: (conversationId: string, image: { id: string; type: string }) => {
      const ext = ({ 'image/png': '.png', 'image/jpeg': '.jpg', 'image/webp': '.webp' } as Record<string, string>)[image.type];
      if (!ext || !/^img_[0-9a-f]{32}$/.test(image.id) || !/^[0-9a-zA-Z-]{8,64}$/.test(conversationId)) return null;
      const uri = `file:///data/user/0/app.askorus/files/photos/${conversationId}/made-${image.id}${ext}`;
      state.targets.push(uri);
      return uri;
    },
    checkMade: (uri: string) => !state.bad.includes(uri),
    mockMadeFiles: state,
  };
});

// Saving a picture into the phone's pictures (#14): a test reads what was saved, or makes it fail.
jest.mock('@/ask/photoSave', () => {
  const state = { available: true, saved: [] as string[], fail: null as string | null };
  return {
    __esModule: true,
    savingAvailable: () => state.available,
    savePicture: async (png: string) => {
      if (state.fail) throw Object.assign(new Error(state.fail), { code: state.fail });
      state.saved.push(png);
    },
    // A made picture (#14) is saved by its address.
    saveMadePicture: async (uri: string) => {
      if (state.fail) throw Object.assign(new Error(state.fail), { code: state.fail });
      state.saved.push(uri);
    },
    mockPhotoSave: state,
  };
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
  const WebView = (props: Record<string, unknown>) => createElement(View, { ...props, testID: props.testID ?? 'math-webview' });
  return { __esModule: true, default: WebView, WebView };
});

// Screens are tested still: the phone 'asks for less motion', so the live
// motions (ui/motion.tsx) show their final look and run no loops.
jest.spyOn(require('react-native').AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(true);

setLang('en');

beforeEach(() => {
  resetRefused();
  mockSecure.clear();
  // The person signed in to Tailscale on this phone before (tailnet/node):
  // the screens under test are those of a phone that can reach its PCs.
  mockSecure.set('tailnet.on.v1', '1');
  resetMockTailnet();
  resetMockPhotos();
  resetTailnet();
  for (const m of mockSQLite.values()) m.clear();
  conversations.reset();
  resetPCs();
  resetPCStatus();
  resetPCUsage();
  resetAgentUpdate();
});
