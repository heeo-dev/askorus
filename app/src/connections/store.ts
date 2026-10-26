// Saved connections. Each has a `kind` so later phases can add other
// kinds next to the PC agent (extension principle 8): handoff (official
// app), direct (in-app), cloud-pc. Tokens live in the secure store under
// their own key and never in the list itself (N8).

import * as Crypto from 'expo-crypto';
import * as SecureStore from 'expo-secure-store';

export type ConnectionKind = 'pc';

export type Connection = {
  id: string;
  kind: ConnectionKind;
  name: string; // the PC's host name, editable later
  host: string; // MagicDNS name
  port: number; // serve HTTPS port
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
   * and its token, so one PC never shows twice.
   */
  async add(c: Omit<Connection, 'id'>, token: string): Promise<Connection> {
    const list = await this.list();
    const old = list.find((x) => x.kind === c.kind && x.host === c.host && x.port === c.port);
    const conn: Connection = { ...c, id: old?.id ?? newId() };
    await this.kv.set(tokenKey(conn.id), token);
    const next = list.filter((x) => x.id !== conn.id).concat(conn);
    await this.kv.set(listKey, JSON.stringify(next));
    return conn;
  }

  async rename(id: string, name: string): Promise<void> {
    const list = await this.list();
    await this.kv.set(listKey, JSON.stringify(list.map((c) => (c.id === id ? { ...c, name } : c))));
  }

  async remove(id: string): Promise<void> {
    await this.kv.remove(tokenKey(id));
    const list = await this.list();
    await this.kv.set(listKey, JSON.stringify(list.filter((c) => c.id !== id)));
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
