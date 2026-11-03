// Milestone B: the conversations kept on the phone (history.ts): one entry
// each and a list of heads, at most 100 and about 20 MB (the oldest go,
// never the one just kept), unreadable data counts as nothing, and 'delete
// all' leaves no conversation behind.

import type { Conversation } from './conversations';
import { forget, forgetAll, type HistoryKV, keep, loadConversation, loadHeads, maxBytes, maxKept, utf8Bytes } from './history';

const memory = () => {
  const mem = new Map<string, string>();
  const kv: HistoryKV = {
    get: async (k) => mem.get(k) ?? null,
    set: async (k, v) => void mem.set(k, v),
    remove: async (k) => void mem.delete(k),
    keys: async () => [...mem.keys()],
  };
  return { mem, kv };
};
const conv = (id: string, startedAt: number, title = `q ${id}`, pad = ''): Conversation => ({
  id,
  pcId: 'pc-1',
  startedAt,
  title: title + pad,
  turns: [],
});

test('UTF-8 sizes: ASCII 1, Hangul 3, emoji 4 bytes', () => {
  expect(utf8Bytes('abc')).toBe(3);
  expect(utf8Bytes('é')).toBe(2);
  expect(utf8Bytes('서울')).toBe(6);
  expect(utf8Bytes('😀')).toBe(4);
});

test('a kept conversation comes back whole; its head is listed newest first with its size', async () => {
  const { mem, kv } = memory();
  await keep(conv('a', 1000, '서울 인구는?'), kv);
  await keep(conv('b', 2000), kv);
  const heads = await loadHeads(kv);
  expect(heads.map((h) => h.id)).toEqual(['b', 'a']);
  expect(heads[1]).toEqual({ id: 'a', startedAt: 1000, title: '서울 인구는?', bytes: utf8Bytes(mem.get('conv.v1.a') ?? '') });
  expect(await loadConversation('a', kv)).toEqual(conv('a', 1000, '서울 인구는?'));
  // Kept again: replaced, still one entry.
  await keep(conv('a', 1000, 'changed'), kv);
  expect((await loadHeads(kv)).map((h) => [h.id, h.title])).toEqual([
    ['b', 'q b'],
    ['a', 'changed'],
  ]);
});

test('past 100 conversations the oldest go', async () => {
  const { mem, kv } = memory();
  for (let i = 0; i < maxKept + 3; i++) await keep(conv(`n${i}`, 1000 + i), kv);
  const heads = await loadHeads(kv);
  expect(heads).toHaveLength(maxKept);
  expect(heads.at(-1)?.id).toBe('n3');
  expect(mem.has('conv.v1.n0')).toBe(false);
  expect(mem.has('conv.v1.n3')).toBe(true);
});

test('past about 20 MB the oldest go, but never the one just kept', async () => {
  const { kv } = memory();
  const big = 'x'.repeat(Math.ceil(maxBytes / 3));
  await keep(conv('a', 1000, 'a', big), kv);
  await keep(conv('b', 2000, 'b', big), kv);
  await keep(conv('c', 3000, 'c', big), kv);
  expect((await loadHeads(kv)).map((h) => h.id)).toEqual(['c', 'b']);
  // An old conversation kept again (it is on screen) stays, though it is the oldest.
  await keep(conv('a', 1000, 'a', big), kv);
  expect((await loadHeads(kv)).map((h) => h.id)).toEqual(['c', 'a']);
});

test('forget removes one; forget all removes every conversation, listed or not', async () => {
  const { mem, kv } = memory();
  await keep(conv('a', 1000), kv);
  await keep(conv('b', 2000), kv);
  expect((await forget('a', kv)).map((h) => h.id)).toEqual(['b']);
  expect(await loadConversation('a', kv)).toBeUndefined();
  mem.set('conv.v1.orphan', '{}');
  await forgetAll(kv);
  expect([...mem.entries()]).toEqual([['index.v1', '[]']]);
});

test('unreadable data counts as nothing', async () => {
  const { mem, kv } = memory();
  for (const raw of ['not json', '{}', 'null', '"x"', '[{"id":1}]']) {
    mem.set('index.v1', raw);
    expect([raw, await loadHeads(kv)]).toEqual([raw, []]);
  }
  mem.set('index.v1', '[{"id":"a","startedAt":1,"title":"t","bytes":2},{"id":""},{"title":"x"}]');
  expect((await loadHeads(kv)).map((h) => h.id)).toEqual(['a']);
  for (const raw of ['not json', 'null', '{"id":"other","turns":[]}', '{"id":"a"}']) {
    mem.set('conv.v1.a', raw);
    expect([raw, await loadConversation('a', kv)]).toEqual([raw, undefined]);
  }
  const broken: HistoryKV = { get: async () => Promise.reject(new Error('disk')), set: async () => {}, remove: async () => {}, keys: async () => [] };
  expect(await loadHeads(broken)).toEqual([]);
  expect(await loadConversation('a', broken)).toBeUndefined();
});
