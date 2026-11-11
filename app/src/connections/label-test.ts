// #53: telling paired PCs apart. A PC's name is its host name, and two PCs
// can carry the same one (a Windows PC and the Linux inside it).

import { cleanPCName, maxPCName, osName, pcDetail, pcLabel, sharesName } from './label';
import { pairWithPC } from './pair';
import { type Connection, type KeyValue, ConnectionStore } from './store';

const pc = (over: Partial<Connection>): Connection => ({
  id: 'a',
  kind: 'pc',
  name: 'DESKTOP-ONE',
  host: 'desktop-one.tail0.ts.net',
  port: 8790,
  deviceId: 'd',
  deviceName: 'Phone',
  pairedAt: 'x',
  ...over,
});

function memory(): KeyValue {
  const m = new Map<string, string>();
  return {
    get: async (k) => m.get(k) ?? null,
    set: async (k, v) => void m.set(k, v),
    remove: async (k) => void m.delete(k),
  };
}

test('a system reads as people call it; one not known reads as nothing', () => {
  expect(osName('windows')).toBe('Windows');
  expect(osName('linux')).toBe('Linux');
  expect(osName('darwin')).toBe('macOS');
  expect(osName('Linux')).toBe('Linux');
  expect(osName('plan9')).toBe('');
  expect(osName(undefined)).toBe('');
});

test('a single PC needs nothing beside its name', () => {
  const one = pc({ os: 'windows' });
  expect(pcDetail(one, [one])).toBe('');
  expect(pcLabel(one, [one])).toBe('DESKTOP-ONE');
  expect(pcDetail(one, null)).toBe('');
});

test('several PCs show their system; PCs that share a name also show the first part of their address', () => {
  const win = pc({ id: 'a', os: 'windows' });
  const wsl = pc({ id: 'b', os: 'linux', host: 'desktop-wsl.tail0.ts.net' });
  const mac = pc({ id: 'c', name: 'Studio', os: 'darwin', host: 'studio.tail0.ts.net' });
  const old = pc({ id: 'd', name: 'Attic', host: 'attic.tail0.ts.net' }); // paired before the app kept the system
  const list = [win, wsl, mac, old];
  expect(sharesName(win, list)).toBe(true);
  expect(sharesName(mac, list)).toBe(false);
  expect(pcDetail(win, list)).toBe('Windows · desktop-one');
  expect(pcDetail(wsl, list)).toBe('Linux · desktop-wsl');
  expect(pcLabel(wsl, list)).toBe('DESKTOP-ONE (Linux · desktop-wsl)');
  expect(pcLabel(mac, list)).toBe('Studio (macOS)');
  expect(pcLabel(old, list)).toBe('Attic');
  // The same name in another case is still the same name.
  expect(sharesName(pc({ id: 'e', name: 'studio' }), list)).toBe(true);
});

test('a typed name is trimmed, printable and at most 40 characters', () => {
  expect(cleanPCName('  WSL  ')).toBe('WSL');
  expect(cleanPCName('집 PC')).toBe('집 PC');
  expect(cleanPCName('a\u0000b​c')).toBe('abc');
  expect(cleanPCName('   ')).toBe('');
  expect(Array.from(cleanPCName('x'.repeat(100)))).toHaveLength(maxPCName);
});

test('the store keeps a system once, and a name the person gave stays when the PC is paired again', async () => {
  const store = new ConnectionStore(memory());
  const { id: _id, ...fresh } = pc({});
  const first = await store.add(fresh, 'token-1');
  expect(first.os).toBeUndefined();
  expect(await store.setOS(first.id, 'linux')).toBe(true);
  expect(await store.setOS(first.id, 'linux')).toBe(false); // nothing changed
  expect(await store.setOS(first.id, '')).toBe(false);
  expect(await store.setOS('nobody', 'linux')).toBe(false);
  expect((await store.get(first.id))?.os).toBe('linux');

  await store.rename(first.id, 'WSL');
  expect(await store.get(first.id)).toMatchObject({ name: 'WSL', named: true, os: 'linux' });
  // Pairing the same address again: the same entry, the new token, the person's name.
  const again = await store.add({ ...fresh, os: 'linux' }, 'token-2');
  expect(again).toMatchObject({ id: first.id, name: 'WSL', named: true });
  expect(await store.token(first.id)).toBe('token-2');
  expect(await store.list()).toHaveLength(1);

  // A PC nobody renamed takes its own name again.
  const other = await store.add({ ...fresh, host: 'other.tail0.ts.net', name: 'OLD' }, 't');
  const renamedByPC = await store.add({ ...fresh, host: 'other.tail0.ts.net', name: 'NEW' }, 't');
  expect(renamedByPC).toMatchObject({ id: other.id, name: 'NEW' });
  expect(renamedByPC.named).toBeUndefined();
});

test('pairing keeps the system the PC reports', async () => {
  const store = new ConnectionStore(memory());
  const answers: Record<string, unknown> = {
    '/v1/pair': { requestId: 'r1', pollAfterMs: 1, apiVersion: 1 },
    '/v1/pair/status': { status: 'approved', token: 'tok', deviceId: 'dev', deviceName: 'Phone', apiVersion: 1 },
    '/v1/info': { name: 'askorus-agent', version: '0.1.0', apiVersion: 1, hostname: 'MACBOOK', os: 'darwin', agents: [], capabilities: [], reserved: [] },
  };
  const fetch = (async (u: RequestInfo | URL) => {
    const path = new URL(String(u)).pathname;
    return new Response(JSON.stringify(answers[path] ?? {}), { status: path in answers ? 200 : 404, headers: { 'Content-Type': 'application/json' } });
  }) as typeof globalThis.fetch;
  const conn = await pairWithPC({ host: 'macbook.tail0.ts.net', port: 8790, code: 'CODE' }, 'Phone', { store, fetch, sleep: async () => undefined });
  expect(conn).toMatchObject({ name: 'MACBOOK', os: 'darwin' });
});

test('a PC that was unpaired takes its id back when it is paired again at the same address', async () => {
  const kv = memory();
  const store = new ConnectionStore(kv);
  const { id: _id, ...fresh } = pc({});
  const first = await store.add(fresh, 'token-1');
  const other = await store.add({ ...fresh, host: 'other.tail0.ts.net' }, 'token-o');
  await store.remove(first.id);
  expect(await store.list()).toHaveLength(1);
  expect(await store.token(first.id)).toBeNull();
  // What is kept of it holds no token and no name.
  expect(await kv.get('connections.gone.v1')).toBe(JSON.stringify([{ id: first.id, host: fresh.host, port: fresh.port }]));

  const again = await store.add(fresh, 'token-2');
  expect(again.id).toBe(first.id);
  expect(await store.token(first.id)).toBe('token-2');
  expect(await kv.get('connections.gone.v1')).toBe('[]');
  // Another address, or another port, is another PC.
  await store.remove(again.id);
  expect((await store.add({ ...fresh, port: 9000 }, 't')).id).not.toBe(first.id);
  expect((await store.add({ ...fresh, host: 'new.tail0.ts.net' }, 't')).id).not.toBe(first.id);
  // A PC that is still paired keeps its own id.
  expect((await store.add({ ...fresh, host: 'other.tail0.ts.net' }, 'token-o2')).id).toBe(other.id);
  // Only the last twenty unpaired PCs are remembered.
  for (let i = 0; i < 25; i++) await store.remove((await store.add({ ...fresh, host: `h${i}.tail0.ts.net` }, 't')).id);
  expect(JSON.parse((await kv.get('connections.gone.v1')) ?? '[]')).toHaveLength(20);
});
