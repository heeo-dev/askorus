// The paired PCs and the current one (S5c). The current PC answers the
// questions and is the one 내 PC shows: the PC the ask prefs name (AskPrefs.pc),
// else the first paired. Picking another saves it in the ask prefs. The
// list is read again whenever a screen that shows it mounts, so a PC paired
// or unpaired elsewhere shows up.

import { useEffect, useSyncExternalStore } from 'react';

import type { AgentClient } from '@/api/client';
import { loadAskPrefs, saveAskPrefs } from '@/ask/prefs';
import { connect } from '@/connections/open';
import { type Connection, ConnectionStore } from '@/connections/store';

type State = { list: Connection[] | null; currentId: string | null };

let state: State = { list: null, currentId: null };
let gen = 0;
const subs = new Set<() => void>();

function set(next: State) {
  state = next;
  for (const fn of [...subs]) fn();
}

function subscribe(fn: () => void) {
  subs.add(fn);
  return () => {
    subs.delete(fn);
  };
}

const pickCurrent = (list: Connection[], pc: string) => (list.some((c) => c.id === pc) ? pc : (list[0]?.id ?? null));

/** reload reads the paired PCs and the current one again. */
export async function reloadPCs(): Promise<void> {
  const g = ++gen;
  const [list, prefs] = await Promise.all([new ConnectionStore().list(), loadAskPrefs()]);
  if (g !== gen) return; // a newer read (or a pick) won
  set({ list, currentId: pickCurrent(list, prefs.pc) });
}

/** pickPC makes id the current PC and saves it in the ask prefs. */
export async function pickPC(id: string): Promise<void> {
  gen++;
  set({ ...state, currentId: id });
  const p = await loadAskPrefs();
  await saveAskPrefs({ ...p, pc: id });
}

/** currentPCId is the current PC as last read (null before the first read or with no PC). */
export function currentPCId(): string | null {
  return state.currentId;
}

export function subscribePCs(fn: () => void): () => void {
  return subscribe(fn);
}

/** resetPCs forgets what was read (for tests). */
export function resetPCs() {
  gen++;
  set({ list: null, currentId: null });
}

export function usePCs(): { list: Connection[] | null; current: Connection | null; pick: (id: string) => Promise<void>; reload: () => Promise<void> } {
  const s = useSyncExternalStore(subscribe, () => state);
  useEffect(() => {
    reloadPCs().catch(() => undefined);
  }, []);
  const current = s.list?.find((c) => c.id === s.currentId) ?? null;
  return { list: s.list, current, pick: pickPC, reload: reloadPCs };
}

/** currentPC reads the current PC afresh, with a client that carries its token; null with no PC. */
export async function currentPC(): Promise<{ conn: Connection; client: AgentClient } | null> {
  const store = new ConnectionStore();
  const [list, prefs] = await Promise.all([store.list(), loadAskPrefs()]);
  const id = pickCurrent(list, prefs.pc);
  return id ? connect(store, id) : null;
}
