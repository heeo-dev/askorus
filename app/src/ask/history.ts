// The conversations kept on the phone (milestone B, P2-6 moved up). Each
// conversation is one entry of an SQLite key-value store in the app's own
// files, and one more entry lists their heads, newest first, so the drawer
// can show them without reading every conversation. At most 100
// conversations and about 20 MB: past that the oldest go (never the one
// kept last, which is the one on screen). Nothing leaves the phone: the
// app is kept out of Android's backup (app.json allowBackup false).
//
// The functions run one after another (queue), so the list never loses a
// write to a race.

import { SQLiteStorage } from 'expo-sqlite/kv-store';

import type { KeyValue } from '@/connections/store';

import type { Conversation, ConversationHead } from './conversations';

export const maxKept = 100;
export const maxBytes = 20 * 1024 * 1024;

const indexKey = 'index.v1';
const convKey = (id: string) => `conv.v1.${id}`;

export type StoredHead = ConversationHead & { bytes: number };

/** HistoryKV is the store: KeyValue, and its keys (so 'delete all' leaves nothing behind). */
export type HistoryKV = KeyValue & { keys(): Promise<string[]> };

let db: SQLiteStorage | null = null;
const open = () => (db ??= new SQLiteStorage('history.db'));

/** phoneHistory is the store on the phone; tests pass their own. */
export const phoneHistory: HistoryKV = {
  get: (k) => open().getItemAsync(k),
  set: (k, v) => open().setItemAsync(k, v),
  remove: async (k) => {
    await open().removeItemAsync(k);
  },
  keys: () => open().getAllKeysAsync(),
};

/** utf8Bytes is how many bytes text takes as UTF-8. */
export function utf8Bytes(text: string): number {
  let n = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (c >= 0xd800 && c < 0xdc00 && i + 1 < text.length) {
      n += 4; // a surrogate pair
      i++;
    } else n += 3;
  }
  return n;
}

function isHead(x: unknown): x is StoredHead {
  if (!x || typeof x !== 'object') return false;
  const h = x as Record<string, unknown>;
  return typeof h.id === 'string' && !!h.id && typeof h.startedAt === 'number' && typeof h.title === 'string' && typeof h.bytes === 'number';
}

async function readIndex(kv: KeyValue): Promise<StoredHead[]> {
  try {
    const v = JSON.parse((await kv.get(indexKey)) ?? '[]');
    return Array.isArray(v) ? v.filter(isHead) : [];
  } catch {
    return [];
  }
}

const writeIndex = (kv: KeyValue, heads: StoredHead[]) => kv.set(indexKey, JSON.stringify(heads));

let line: Promise<unknown> = Promise.resolve();
function queue<T>(job: () => Promise<T>): Promise<T> {
  const next = line.then(job, job);
  line = next.catch(() => undefined);
  return next;
}

/** loadHeads lists the kept conversations, newest first. */
export function loadHeads(kv: KeyValue = phoneHistory): Promise<StoredHead[]> {
  return queue(() => readIndex(kv));
}

/** loadConversation reads one conversation, or undefined when it is gone or unreadable. */
export function loadConversation(id: string, kv: KeyValue = phoneHistory): Promise<Conversation | undefined> {
  return queue(async () => {
    try {
      const c = JSON.parse((await kv.get(convKey(id))) ?? 'null');
      return c && typeof c === 'object' && c.id === id && Array.isArray(c.turns) ? (c as Conversation) : undefined;
    } catch {
      return undefined;
    }
  });
}

/**
 * keep writes a conversation (new, or replacing the one with its id) and
 * returns the heads after it, newest first, with the oldest beyond the
 * limits removed.
 */
export function keep(c: Conversation, kv: KeyValue = phoneHistory): Promise<StoredHead[]> {
  return queue(async () => {
    const text = JSON.stringify(c);
    const head: StoredHead = { id: c.id, startedAt: c.startedAt, title: c.title, bytes: utf8Bytes(text) };
    await kv.set(convKey(c.id), text);
    const heads = [head, ...(await readIndex(kv)).filter((h) => h.id !== c.id)].sort((a, b) => b.startedAt - a.startedAt);
    // Over the limits: the oldest go, but never the one just kept.
    let total = heads.reduce((n, h) => n + h.bytes, 0);
    const out = [...heads];
    for (let i = out.length - 1; i >= 0 && (out.length > maxKept || total > maxBytes); i--) {
      if (out[i].id === c.id) continue;
      total -= out[i].bytes;
      await kv.remove(convKey(out[i].id));
      out.splice(i, 1);
    }
    await writeIndex(kv, out);
    return out;
  });
}

/** forget removes one conversation and returns the heads left. */
export function forget(id: string, kv: KeyValue = phoneHistory): Promise<StoredHead[]> {
  return queue(async () => {
    await kv.remove(convKey(id));
    const heads = (await readIndex(kv)).filter((h) => h.id !== id);
    await writeIndex(kv, heads);
    return heads;
  });
}

/** forgetAll removes every kept conversation, listed or not. */
export function forgetAll(kv: HistoryKV = phoneHistory): Promise<void> {
  return queue(async () => {
    for (const k of await kv.keys()) if (k.startsWith('conv.')) await kv.remove(k);
    await writeIndex(kv, []);
  });
}
