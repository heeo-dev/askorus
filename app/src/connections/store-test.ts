import { type KeyValue, ConnectionStore } from './store';

jest.mock('expo-crypto', () => {
  let n = 0;
  return { randomUUID: () => `0000000${++n}-0000-4000-8000-000000000000` };
});

function memory(): KeyValue & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    get: async (k) => data.get(k) ?? null,
    set: async (k, v) => void data.set(k, v),
    remove: async (k) => void data.delete(k),
  };
}

const pc = {
  kind: 'pc' as const,
  name: 'DESKTOP',
  host: 'desktop-x.tail1.ts.net',
  port: 8790,
  deviceId: 'd1',
  deviceName: 'S23',
  pairedAt: '2026-10-27T00:00:00Z',
};

test('tokens are kept apart from the connection list', async () => {
  const kv = memory();
  const s = new ConnectionStore(kv);
  const c = await s.add(pc, 'secret-token');
  expect(await s.token(c.id)).toBe('secret-token');
  expect(kv.data.get('connections.v1')).not.toContain('secret-token');
  expect(await s.list()).toEqual([{ ...pc, id: c.id }]);
});

test('pairing the same PC again replaces it', async () => {
  const s = new ConnectionStore(memory());
  const a = await s.add(pc, 'old');
  const b = await s.add({ ...pc, deviceId: 'd2' }, 'new');
  expect(b.id).toBe(a.id);
  expect(await s.list()).toHaveLength(1);
  expect((await s.get(a.id))?.deviceId).toBe('d2');
  expect(await s.token(a.id)).toBe('new');
});

test('two PCs stay apart (F7)', async () => {
  const s = new ConnectionStore(memory());
  const a = await s.add(pc, 't1');
  const b = await s.add({ ...pc, host: 'laptop.tail1.ts.net', name: 'LAPTOP' }, 't2');
  expect(a.id).not.toBe(b.id);
  expect(await s.token(a.id)).toBe('t1');
  expect(await s.token(b.id)).toBe('t2');
});

test('removing a PC drops its token', async () => {
  const kv = memory();
  const s = new ConnectionStore(kv);
  const c = await s.add(pc, 'secret');
  await s.remove(c.id);
  expect(await s.list()).toEqual([]);
  expect(await s.token(c.id)).toBeNull();
});

test('ids are valid secure-store keys', async () => {
  const s = new ConnectionStore(memory());
  const c = await s.add(pc, 't');
  expect(`token.${c.id}`).toMatch(/^[\w.-]+$/);
});

test('a damaged list reads as empty', async () => {
  const kv = memory();
  kv.data.set('connections.v1', '{not json');
  expect(await new ConnectionStore(kv).list()).toEqual([]);
  kv.data.set('connections.v1', JSON.stringify([{ id: 'x', kind: 'other' }, { ...pc, id: 'y' }]));
  expect((await new ConnectionStore(kv).list()).map((c) => c.id)).toEqual(['y']);
});
