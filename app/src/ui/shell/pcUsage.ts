// The current PC's /v1/info for the top bar's usage button (#20, decision
// 16): every screen with a top bar shows what is left of the AIs' plans.
// Read when the shell starts, on a new current PC, when the app comes to
// the front, every 5 minutes while it is in front (the agent keeps the
// usage 5 minutes too), and when the usage sheet opens. Screens that read
// /v1/info themselves (the ask screen, Manage AIs) hand it over with
// publishInfo, and a read within freshMs of that is skipped. A failed read
// keeps the last info: the bars stay as they were.

import { useSyncExternalStore } from 'react';

import type { Info } from '@/api/types';

import { currentPC, currentPCId, subscribePCs } from './pcs';
import { refreshPCStatus } from './pcStatus';

export type PCUsage = { pcId: string; info: Info; at: number };

export const usageEveryMs = 5 * 60 * 1000;
const freshMs = 15000;

let infos = new Map<string, PCUsage>();
const inflight = new Map<string, Promise<void>>();
let epoch = 0;
const subs = new Set<() => void>();

function put(u: PCUsage) {
  infos = new Map(infos).set(u.pcId, u);
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

/** publishInfo hands over a /v1/info a screen read of a PC. */
export function publishInfo(pcId: string, info: Info): void {
  put({ pcId, info, at: Date.now() });
}

/** refreshPCUsage reads the current PC's info, unless one is fresh (force: anyway). */
export async function refreshPCUsage(o: { force?: boolean } = {}): Promise<void> {
  const pc = await currentPC();
  if (!pc) return;
  const id = pc.conn.id;
  const last = infos.get(id);
  if (!o.force && last && Date.now() - last.at < freshMs) return;
  const running = inflight.get(id);
  if (running) return running;
  const e0 = epoch;
  const p = pc.client
    .info()
    .then((info) => {
      if (epoch === e0) put({ pcId: id, info, at: Date.now() });
    })
    .catch(() => undefined)
    .finally(() => {
      if (epoch === e0) inflight.delete(id);
    });
  inflight.set(id, p);
  return p;
}

/**
 * refreshStatusThenUsage reads the current PC's status first (pcStatus.ts,
 * at most once at a time), then its usage: a token the PC refused then
 * sends nothing more (R-115).
 */
export function refreshStatusThenUsage(o: { force?: boolean } = {}): Promise<void> {
  return refreshPCStatus()
    .catch(() => undefined)
    .then(() => refreshPCUsage(o))
    .catch(() => undefined);
}

/** usePCUsage is the current PC's last info, null before one was read or with no PC. */
export function usePCUsage(): PCUsage | null {
  return useSyncExternalStore(subscribe, () => {
    const id = currentPCId();
    return id ? (infos.get(id) ?? null) : null;
  });
}

/** resetPCUsage forgets every info (for tests). */
export function resetPCUsage() {
  epoch++;
  infos = new Map();
  inflight.clear();
  for (const fn of [...subs]) fn();
}
