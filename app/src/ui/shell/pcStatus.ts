// Whether the current PC answers and which of its sessions wait for input
// (S5c): the dot on the header's 내 PC and the drawer's PC block. Read from
// GET /v1/sessions of the current PC (no filter) when the shell starts,
// when the app comes to the front, when the drawer opens and every 30 s
// while the app is in front. The 내 PC screen fetches the same list; it
// hands each full (unfiltered) list it gets to publishSessions, and a
// refresh within 30 s of that is skipped.
//
// Waiting = the agent's activity 'blocked' (claude agents state: a
// background session waiting for input), except the sessions of asks.

import { useSyncExternalStore } from 'react';

import type { Session } from '@/api/types';
import { CancelledError } from '@/api/client';
import { type Cause, cause } from '@/connections/reach';

import { currentPC, currentPCId, subscribePCs } from './pcs';

export type PCStatus = { pcId: string; state: 'checking' | 'ok' | 'error'; cause?: Cause; waiting: Session[]; at: number };

export const statusEveryMs = 30000;

let statuses = new Map<string, PCStatus>();
let published = new Map<string, number>(); // when a screen last handed over a list
let readAt = new Map<string, number>(); // when this file last read a PC's list itself
const freshMs = 5000; // a read this young is not repeated (start, a new current PC, the drawer)
const inflight = new Map<string, Promise<void>>();
let epoch = 0; // a reset drops the answers of reads still running
const subs = new Set<() => void>();

function put(s: PCStatus) {
  statuses = new Map(statuses).set(s.pcId, s);
  for (const fn of [...subs]) fn();
}

function subscribe(fn: () => void) {
  subs.add(fn);
  const off = subscribePCs(fn);
  return () => {
    subs.delete(fn);
    off();
  };
}

/** waitingOf picks the sessions that wait for input, leaving out asks. */
export function waitingOf(sessions: Session[]): Session[] {
  return sessions.filter((x) => x.activity === 'blocked' && !(x.badges ?? []).includes('ask'));
}

function ok(pcId: string, sessions: Session[]) {
  put({ pcId, state: 'ok', waiting: waitingOf(sessions), at: Date.now() });
}

function failed(pcId: string, e: unknown) {
  if (e instanceof CancelledError) return; // the screen stopped waiting; nothing learned
  put({ pcId, state: 'error', cause: cause(e), waiting: [], at: Date.now() });
}

/** publishSessions hands over a full session list of a PC (no search or state filter). */
export function publishSessions(pcId: string, sessions: Session[]): void {
  published = new Map(published).set(pcId, Date.now());
  ok(pcId, sessions);
}

/** publishError hands over a failed GET /v1/sessions of a PC. */
export function publishError(pcId: string, e: unknown): void {
  if (e instanceof CancelledError) return;
  published = new Map(published).set(pcId, Date.now());
  failed(pcId, e);
}

/** refreshPCStatus reads the current PC's sessions, unless a screen just did (force: anyway). */
export async function refreshPCStatus(o: { force?: boolean } = {}): Promise<void> {
  const pc = await currentPC();
  if (!pc) return;
  const id = pc.conn.id;
  const last = published.get(id);
  if (!o.force && last !== undefined && Date.now() - last < statusEveryMs) return;
  const own = readAt.get(id);
  if (!o.force && own !== undefined && Date.now() - own < freshMs) return;
  const running = inflight.get(id);
  if (running) return running;
  if (!statuses.has(id)) put({ pcId: id, state: 'checking', waiting: [], at: Date.now() });
  const e0 = epoch;
  const p = pc.client
    .sessions()
    .then((list) => {
      if (epoch !== e0) return;
      readAt = new Map(readAt).set(id, Date.now());
      ok(id, list.sessions);
    })
    .catch((e) => {
      if (epoch !== e0) return;
      readAt = new Map(readAt).set(id, Date.now());
      failed(id, e);
    })
    .finally(() => {
      if (epoch === e0) inflight.delete(id);
    });
  inflight.set(id, p);
  return p;
}

/** usePCStatus is the current PC's status, null before it was first read or with no PC. */
export function usePCStatus(): PCStatus | null {
  return useSyncExternalStore(subscribe, () => {
    const id = currentPCId();
    return id ? (statuses.get(id) ?? null) : null;
  });
}

/** resetPCStatus forgets every status (for tests). */
export function resetPCStatus() {
  epoch++;
  statuses = new Map();
  published = new Map();
  readAt = new Map();
  inflight.clear();
  for (const fn of [...subs]) fn();
}
