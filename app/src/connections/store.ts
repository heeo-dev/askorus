// Saved connections. Each has a `kind` so later phases can add other
// kinds next to the PC agent (extension principle 8): handoff (official
// app), direct (in-app), cloud-pc. Tokens live in the secure store under
// their own key and never in the list itself (N8).

import * as Crypto from 'expo-crypto';
import * as SecureStore from 'expo-secure-store';

import { agentBase, forgetRefused } from '@/api/refused';

export type ConnectionKind = 'pc';

export type Connection = {
  id: string;
  kind: ConnectionKind;
  name: string; // the PC's host name until the person gives it another (#53)
  os?: string; // the PC's system as its agent says it (windows, linux, darwin); absent until it was read (#53)
  named?: boolean; // the person gave the name: pairing again keeps it (#53)
  host: string; // the name of the PC agent's own Tailscale node (MagicDNS)
  port: number; // the port it listens on there
  deviceId: string; // this phone's id on that PC
  deviceName: string;
  pairedAt: string; // RFC 3339
};

/** The few secure-store calls used here, so tests can swap the store. */
export type KeyValue = {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
};

export const secureKeyValue: KeyValue = {
  get: (k) => SecureStore.getItemAsync(k),
  set: (k, v) => SecureStore.setItemAsync(k, v),
  remove: (k) => SecureStore.deleteItemAsync(k),
};

const listKey = 'connections.v1';
// PCs that were unpaired, newest first: their id and address only, no token.
// Pairing the same address again takes the id back, so the conversations
// asked on that PC go on with it instead of staying read-only (#53).
const goneKey = 'connections.gone.v1';
const maxGone = 20;
type Gone = { id: string; host: string; port: number };
const tokenKey = (id: string) => `token.${id}`;

export class ConnectionStore {
  constructor(private readonly kv: KeyValue = secureKeyValue) {}

  async list(): Promise<Connection[]> {
    const raw = await this.kv.get(listKey);
    if (!raw) return [];
    try {
      const v = JSON.parse(raw);
      return Array.isArray(v) ? v.filter(isConnection) : [];
    } catch {
      return [];
    }
  }

  async get(id: string): Promise<Connection | undefined> {
    return (await this.list()).find((c) => c.id === id);
  }

  async token(id: string): Promise<string | null> {
    return this.kv.get(tokenKey(id));
  }

  /**
   * Saves a new pairing. Pairing the same PC again replaces the old entry
   * and its token, so one PC never shows twice; a name the person gave it
   * stays.
   */
  async add(c: Omit<Connection, 'id'>, token: string): Promise<Connection> {
    const list = await this.list();
    const old = list.find((x) => x.kind === c.kind && x.host === c.host && x.port === c.port);
    // Unpaired before: the same PC comes back under its id.
    const gone = old ? [] : await this.gone();
    const back = gone.find((g) => g.host === c.host && g.port === c.port && !list.some((x) => x.id === g.id));
    const conn: Connection = { ...c, ...(old?.named ? { name: old.name, named: true } : {}), id: old?.id ?? back?.id ?? newId() };
    if (back) await this.kv.set(goneKey, JSON.stringify(gone.filter((g) => g !== back)));
    await this.kv.set(tokenKey(conn.id), token);
    // Paired (again): whatever that agent refused before no longer counts.
    forgetRefused(agentBase(c.host, c.port));
    const next = list.filter((x) => x.id !== conn.id).concat(conn);
    await this.kv.set(listKey, JSON.stringify(next));
    return conn;
  }

  /** rename gives a PC the name the person typed. */
  async rename(id: string, name: string): Promise<void> {
    const list = await this.list();
    await this.kv.set(listKey, JSON.stringify(list.map((c) => (c.id === id ? { ...c, name, named: true } : c))));
  }

  /** setOS keeps the system a PC's agent reported; it reports whether anything changed. */
  async setOS(id: string, os: string): Promise<boolean> {
    const list = await this.list();
    if (!os || !list.some((c) => c.id === id && c.os !== os)) return false;
    await this.kv.set(listKey, JSON.stringify(list.map((c) => (c.id === id ? { ...c, os } : c))));
    return true;
  }

  async remove(id: string): Promise<void> {
    await this.kv.remove(tokenKey(id));
    const list = await this.list();
    const c = list.find((x) => x.id === id);
    await this.kv.set(listKey, JSON.stringify(list.filter((x) => x.id !== id)));
    if (!c) return;
    const gone = (await this.gone()).filter((g) => g.id !== id && !(g.host === c.host && g.port === c.port));
    await this.kv.set(goneKey, JSON.stringify([{ id, host: c.host, port: c.port }, ...gone].slice(0, maxGone)));
  }

  /** forgetGone drops what is kept of unpaired PCs; clearing the whole history does this (docs/privacy.md). */
  async forgetGone(): Promise<void> {
    await this.kv.remove(goneKey);
  }

  private async gone(): Promise<Gone[]> {
    try {
      const v = JSON.parse((await this.kv.get(goneKey)) ?? '[]');
      return Array.isArray(v) ? v.filter((g) => g && typeof g.id === 'string' && typeof g.host === 'string' && typeof g.port === 'number') : [];
    } catch {
      return [];
    }
  }
}

function isConnection(v: unknown): v is Connection {
  const c = v as Connection;
  return (
    !!c &&
    typeof c.id === 'string' &&
    c.kind === 'pc' &&
    typeof c.host === 'string' &&
    typeof c.port === 'number' &&
    typeof c.name === 'string'
  );
}

// Secure-store keys allow letters, digits, ".", "-" and "_".
function newId(): string {
  return Crypto.randomUUID().replace(/-/g, '').slice(0, 16);
}
