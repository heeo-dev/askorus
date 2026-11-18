// An agent on the PC that is older than this app (11-18, before #35 brings
// an update button). The store updates the app on its own; the PC's agent
// only when someone runs the install line on the PC again. Each release
// carries the app and the agent at one version, so an agent older than the
// app has a newer one waiting on the releases page. The phone asks nothing
// of the internet for this (N1): it compares the two versions it already has.
//
// The notice can be put away for one PC; it comes back when the agent's or
// the app's version changes.

import Constants from 'expo-constants';
import { useEffect, useSyncExternalStore } from 'react';

import { type KeyValue, secureKeyValue } from '@/connections/store';

export type Version = readonly [number, number, number];

/** parseVersion reads major.minor.patch at the start ('0.4.0-dev+f0a576d' is 0.4.0); null when there is none. */
export function parseVersion(v: string | null | undefined): Version | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec((v ?? '').trim());
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

function before(a: Version, b: Version): boolean {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] < b[i];
  return false;
}

/**
 * agentBehind: the agent's version is older than the app's. False when either
 * is not a version (a build run by hand, tests) and when the agent is newer.
 */
export function agentBehind(agent: string | null | undefined, app: string | null | undefined): boolean {
  const a = parseVersion(agent);
  const b = parseVersion(app);
  return !!a && !!b && before(a, b);
}

/** This app's version (app.json's); '' under tests. */
export function appVersion(): string {
  return Constants.expoConfig?.version ?? '';
}

/** The one line that installs the agent, or updates it when run again (#37, #52). */
export const installLine = {
  windows: 'irm https://github.com/heeo-dev/askorus/releases/latest/download/install.ps1 | iex',
  unix: 'curl -fsSL https://github.com/heeo-dev/askorus/releases/latest/download/install.sh | sh',
} as const;

/** The line for the PC's system, and where it is typed. */
export function updateLine(os: string): { shell: 'powershell' | 'terminal'; line: string } {
  return os === 'windows' ? { shell: 'powershell', line: installLine.windows } : { shell: 'terminal', line: installLine.unix };
}

// The notices put away: PC id → 'agent>app' of the versions it was about.
const key = 'agentUpdate.v1';
let away: Record<string, string> | null = null;
let reading: Promise<void> | null = null;
let store: KeyValue = secureKeyValue;
const subs = new Set<() => void>();

function emit() {
  for (const fn of [...subs]) fn();
}

function subscribe(fn: () => void) {
  subs.add(fn);
  return () => {
    subs.delete(fn);
  };
}

function read(): Promise<void> {
  reading ??= (async () => {
    let got: Record<string, string> = {};
    try {
      const raw = await store.get(key);
      const v: unknown = raw ? JSON.parse(raw) : {};
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        got = Object.fromEntries(Object.entries(v).filter((e): e is [string, string] => typeof e[1] === 'string'));
      }
    } catch {
      // A store that cannot be read shows the notice again; nothing is lost.
    }
    away = got;
    emit();
  })();
  return reading;
}

const pair = (agent: string, app: string) => `${agent}>${app}`;

/**
 * useAgentUpdate: whether to show the notice for this PC, and how to put it
 * away. Nothing shows until the store was read, so a notice put away does not
 * flash on opening the app.
 */
export function useAgentUpdate(pcId: string | null | undefined, agent: string | null | undefined, app: string): { show: boolean; putAway: () => void } {
  const put = useSyncExternalStore(subscribe, () => away);
  useEffect(() => {
    if (!away) read().catch(() => undefined);
  }, []);
  const behind = !!pcId && agentBehind(agent, app);
  const show = behind && put !== null && put[pcId ?? ''] !== pair(agent ?? '', app);
  const putAway = () => {
    if (!pcId || !agent) return;
    away = { ...(away ?? {}), [pcId]: pair(agent, app) };
    emit();
    store.set(key, JSON.stringify(away)).catch(() => undefined);
  };
  return { show, putAway };
}

/** For tests: forget what was read, and read from kv from now on. */
export function resetAgentUpdate(kv: KeyValue = secureKeyValue) {
  away = null;
  reading = null;
  store = kv;
}
