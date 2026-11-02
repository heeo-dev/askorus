// The questions asked in this run of the app (S5c), for the drawer's
// history and the Ask screen's '최근 질문'. They live in memory only: the app
// keeps nothing on the phone until P2-6 (phone history), and the drawer
// says they go when the app closes. At most 50, newest first.
//
// The Ask screen keeps the current conversation's turns in its own state
// and only tells the store its head (setCurrent). Starting a new question,
// or opening another one, archives the current turns with save(). The
// drawer asks for a conversation with requestOpen(); the Ask screen takes
// the request (takeOpenRequest) and swaps the turns.

import { useSyncExternalStore } from 'react';

import type { Turn } from '@/ask/thread';
import { currentLang, type Lang, t } from '@/i18n';

export type Conversation = { id: string; pcId: string; startedAt: number; title: string; turns: Turn[] };
export type ConversationHead = { id: string; startedAt: number; title: string };
export type DayGroup = { key: string; label: string; items: { id: string; title: string; current: boolean }[] };

export const maxConversations = 50;

type Snapshot = { list: Conversation[]; current: ConversationHead | null; openRequest: string | null };

let snap: Snapshot = { list: [], current: null, openRequest: null };
const subs = new Set<() => void>();

function set(next: Partial<Snapshot>) {
  snap = { ...snap, ...next };
  for (const fn of [...subs]) fn();
}

export const conversations = {
  list: (): Conversation[] => snap.list,
  get: (id: string): Conversation | undefined => snap.list.find((c) => c.id === id),
  /** save adds a conversation or replaces the one with its id; newest first, at most 50. */
  save(c: Conversation) {
    const list = [c, ...snap.list.filter((x) => x.id !== c.id)].sort((a, b) => b.startedAt - a.startedAt);
    set({ list: list.slice(0, maxConversations) });
  },
  remove(id: string) {
    set({ list: snap.list.filter((c) => c.id !== id), openRequest: snap.openRequest === id ? null : snap.openRequest });
  },
  clear() {
    set({ list: [], current: null, openRequest: null });
  },
  setCurrent(h: ConversationHead | null) {
    set({ current: h ? { id: h.id, startedAt: h.startedAt, title: h.title } : null });
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

/** recentQuestions are the first questions of this run's conversations, newest first, each once. */
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
