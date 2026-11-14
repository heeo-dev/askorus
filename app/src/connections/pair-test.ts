import { AgentError } from '../api/client';
import { cleanDeviceName, pairWithPC } from './pair';
import { type KeyValue, ConnectionStore } from './store';

jest.mock('expo-crypto', () => ({ randomUUID: () => '11111111-2222-4333-8444-555555555555' }));

function memory(): KeyValue {
  const data = new Map<string, string>();
  return {
    get: async (k) => data.get(k) ?? null,
    set: async (k, v) => void data.set(k, v),
    remove: async (k) => void data.delete(k),
  };
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const link = { host: 'desktop-x.tail1.ts.net', port: 8790, code: 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' };
const noSleep = async () => {};
const now = () => new Date('2026-10-27T01:02:03Z');

test('pairing saves the PC under its own host name, with the token apart', async () => {
  const urls: string[] = [];
  const auth: Array<string | undefined> = [];
  const answers = [
    json(202, { requestId: 'r1', status: 'pending', pollAfterMs: 1000 }),
    json(200, { status: 'approved', token: 'secret', deviceId: 'd1', deviceName: 'S23', apiVersion: 1 }),
    json(200, { name: 'askorus-agent', version: '0.1.0', apiVersion: 1, hostname: 'MY-PC' }),
  ];
  const fetchFn = (async (u: RequestInfo | URL, init?: RequestInit) => {
    urls.push(String(u));
    auth.push((init?.headers as Record<string, string>).Authorization);
    return answers.shift()!;
  }) as typeof fetch;
  const store = new ConnectionStore(memory());

  const c = await pairWithPC(link, 'S23', { store, fetch: fetchFn, sleep: noSleep, now });

  expect(urls).toEqual([
    'http://desktop-x.tail1.ts.net:8790/v1/pair',
    'http://desktop-x.tail1.ts.net:8790/v1/pair/status',
    'http://desktop-x.tail1.ts.net:8790/v1/info',
  ]);
  expect(auth).toEqual([undefined, undefined, 'Bearer secret']);
  expect(c).toMatchObject({
    kind: 'pc',
    name: 'MY-PC',
    host: link.host,
    port: 8790,
    deviceId: 'd1',
    deviceName: 'S23',
    pairedAt: '2026-10-27T01:02:03.000Z',
  });
  expect(await store.token(c.id)).toBe('secret');
});

test('if reading the PC name fails after pairing, the host name is used', async () => {
  const answers = [
    json(202, { requestId: 'r1', status: 'pending', pollAfterMs: 1000 }),
    json(200, { status: 'approved', token: 'secret', deviceId: 'd1', deviceName: 'S23', apiVersion: 1 }),
  ];
  const fetchFn = (async () => {
    const a = answers.shift();
    if (!a) throw new TypeError('Network request failed');
    return a;
  }) as typeof fetch;
  const store = new ConnectionStore(memory());
  const c = await pairWithPC(link, 'S23', { store, fetch: fetchFn, sleep: noSleep, now });
  expect(c.name).toBe('desktop-x');
  expect(await store.list()).toHaveLength(1);
});

test('nothing is saved when the PC says no', async () => {
  const answers = [
    json(202, { requestId: 'r1', status: 'pending', pollAfterMs: 1000 }),
    json(403, { error: { code: 'pair_denied', message: 'denied' } }),
  ];
  const fetchFn = (async () => answers.shift()!) as typeof fetch;
  const store = new ConnectionStore(memory());
  const err = await pairWithPC(link, 'S23', { store, fetch: fetchFn, sleep: noSleep }).catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('pair_denied');
  expect(await store.list()).toEqual([]);
});

test('device names are cleaned to what the agent accepts', () => {
  expect(cleanDeviceName('  Galaxy S23 Ultra ')).toBe('Galaxy S23 Ultra');
  expect(cleanDeviceName(`a${String.fromCodePoint(0)}b${String.fromCodePoint(0x200b)}c${String.fromCodePoint(10)}`)).toBe('abc');
  expect(cleanDeviceName('')).toBe('Phone');
  expect(cleanDeviceName(null)).toBe('Phone');
  expect(Array.from(cleanDeviceName('가'.repeat(80)))).toHaveLength(64);
});
