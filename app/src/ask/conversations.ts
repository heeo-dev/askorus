// The conversations for the drawer's history and the Ask screen's '최근
// 질문' (S5c), kept on the phone (milestone B, history.ts): only their heads
// are in memory, newest first; a whole conversation is read when it is
// opened. init reads the heads once when the app starts.
//
// The Ask screen keeps the current conversation's turns in its own state,
// tells the store its head (setCurrent) and saves it as it changes. The
// drawer asks for a conversation with requestOpen(); the Ask screen takes
// the request (takeOpenRequest) and swaps the turns. Removing the current
// conversation (one, or all) leaves a dropped id the Ask screen takes
// (takeDropped) to start afresh; a removed conversation is never saved
// again by a late save.

import { useSyncExternalStore } from 'react';

import type { Turn } from '@/ask/thread';
import { currentLang, type Lang, t } from '@/i18n';

import { forget, forgetAll, type HistoryKV, keep, loadConversation, loadHeads, maxKept, phoneHistory } from './history';

export type Conversation = { id: string; pcId: string; startedAt: number; title: string; turns: Turn[] };
export type ConversationHead = { id: string; startedAt: number; title: string };
export type DayGroup = { key: string; label: string; items: { id: string; title: string; current: boolean }[] };

export const maxConversations = maxKept;

type Snapshot = { list: ConversationHead[]; current: ConversationHead | null; openRequest: string | null; dropped: string | null };

let snap: Snapshot = { list: [], current: null, openRequest: null, dropped: null };
const subs = new Set<() => void>();
let kv: HistoryKV = phoneHistory;
const gone = new Set<string>();

function set(next: Partial<Snapshot>) {
  snap = { ...snap, ...next };
  for (const fn of [...subs]) fn();
}

const headOf = (c: ConversationHead): ConversationHead => ({ id: c.id, startedAt: c.startedAt, title: c.title });

export const conversations = {
  list: (): ConversationHead[] => snap.list,
  /** init reads the kept heads (the phone's store, or kv in tests). */
  async init(store: HistoryKV = phoneHistory) {
    kv = store;
    try {
      const heads = await loadHeads(kv);
      // Saved meanwhile (in this run) comes first; the rest as kept.
      const seen = new Set(snap.list.map((h) => h.id));
      set({ list: [...snap.list, ...heads.filter((h) => !seen.has(h.id) && !gone.has(h.id)).map(headOf)].sort((a, b) => b.startedAt - a.startedAt) });
    } catch {
      // Unreadable: this run starts with an empty history.
    }
  },
  /** load reads a whole conversation, or undefined when it is gone. */
  load: (id: string): Promise<Conversation | undefined> => loadConversation(id, kv),
  /** save keeps a conversation (new, or replacing the one with its id); newest first, at most maxConversations. */
  async save(c: Conversation) {
    if (gone.has(c.id)) return;
    const list = [headOf(c), ...snap.list.filter((x) => x.id !== c.id)].sort((a, b) => b.startedAt - a.startedAt);
    set({ list: list.slice(0, maxConversations) });
    try {
      const heads = await keep(c, kv);
      // What the limits removed goes from the list too.
      const kept = new Set(heads.map((h) => h.id));
      if (snap.list.some((h) => !kept.has(h.id))) set({ list: snap.list.filter((h) => kept.has(h.id)) });
    } catch {
      // Not written: it stays in this run's list.
    }
  },
  /** remove deletes one conversation; when it is the current one, the Ask screen starts afresh. */
  async remove(id: string) {
    gone.add(id);
    const current = snap.current?.id === id;
    set({
      list: snap.list.filter((c) => c.id !== id),
      openRequest: snap.openRequest === id ? null : snap.openRequest,
      ...(current ? { current: null, dropped: id } : {}),
    });
    await forget(id, kv).catch(() => undefined);
  },
  /** removeAll deletes every conversation, the current one too. */
  async removeAll() {
    for (const h of snap.list) gone.add(h.id);
    if (snap.current) gone.add(snap.current.id);
    set({ list: [], openRequest: null, ...(snap.current ? { current: null, dropped: snap.current.id } : {}) });
    await forgetAll(kv).catch(() => undefined);
  },
  /** reset empties the memory only (tests). */
  reset() {
    kv = phoneHistory;
    gone.clear();
    snap = { list: [], current: null, openRequest: null, dropped: null };
    for (const fn of [...subs]) fn();
  },
  setCurrent(h: ConversationHead | null) {
    set({ current: h ? headOf(h) : null });
  },
  current: (): ConversationHead | null => snap.current,
  requestOpen(id: string) {
    set({ openRequest: id });
  },
  /** takeOpenRequest gives the conversation asked for once, then forgets it. */
  takeOpenRequest(): string | null {
    const id = snap.openRequest;
    if (id !== null) set({ openRequest: null });
    return id;
  },
  /** takeDropped gives the removed current conversation once, then forgets it. */
  takeDropped(): string | null {
    const id = snap.dropped;
    if (id !== null) set({ dropped: null });
    return id;
  },
  subscribe(fn: () => void): () => void {
    subs.add(fn);
    return () => {
      subs.delete(fn);
    };
  },
};

export function useConversations(): Snapshot {
  return useSyncExternalStore(conversations.subscribe, () => snap);
}

const dayKey = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

/**
 * dayGroups sorts the history by day, newest first: 오늘, 어제, then
 * '10월 29일'. The current conversation comes first under its day.
 */
export function dayGroups(o: { list: ConversationHead[]; current: ConversationHead | null; now: Date; lang?: Lang }): DayGroup[] {
  const lang = o.lang ?? currentLang();
  const today = dayKey(o.now);
  const yesterday = dayKey(new Date(o.now.getFullYear(), o.now.getMonth(), o.now.getDate() - 1));
  const heads = [...(o.current ? [o.current] : []), ...o.list.filter((c) => c.id !== o.current?.id)];
  const ordered = heads
    .map((h, i) => ({ h, i, current: h.id === o.current?.id }))
    .sort((a, b) => (a.current !== b.current ? (a.current ? -1 : 1) : b.h.startedAt - a.h.startedAt || a.i - b.i));
  const groups = new Map<string, DayGroup & { at: number }>();
  for (const { h, current } of ordered) {
    const d = new Date(h.startedAt);
    const key = dayKey(d);
    let g = groups.get(key);
    if (!g) {
      const label =
        key === today
          ? t('drawer.today', {}, lang)
          : key === yesterday
            ? t('drawer.yesterday', {}, lang)
            : t('drawer.day', { m: d.getMonth() + 1, d: d.getDate() }, lang);
      g = { key, label, items: [], at: new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() };
      groups.set(key, g);
    }
    g.items.push({ id: h.id, title: h.title, current });
  }
  return [...groups.values()].sort((a, b) => b.at - a.at).map(({ at: _at, ...g }) => g);
}

/** recentQuestions are the first questions of the kept conversations, newest first, each once. */
export function recentQuestions(n = 3): string[] {
  const heads = [...(snap.current ? [snap.current] : []), ...snap.list.filter((c) => c.id !== snap.current?.id)].sort(
    (a, b) => b.startedAt - a.startedAt,
  );
  const out: string[] = [];
  for (const h of heads) {
    const q = h.title.trim();
    if (q && !out.includes(q)) out.push(q);
    if (out.length >= n) break;
  }
  return out;
}
