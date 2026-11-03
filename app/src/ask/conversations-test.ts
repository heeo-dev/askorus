// S5c and milestone B: the conversations for the drawer and '최근 질문',
// kept on the phone, at most 100, newest first; heads in memory, a whole
// conversation read when opened; grouped by day for the drawer.

import { act, renderHook } from '@testing-library/react-native';

import { type Conversation, conversations, dayGroups, maxConversations, recentQuestions, useConversations } from './conversations';
import type { HistoryKV } from './history';

const at = (y: number, m: number, d: number, h = 12) => new Date(y, m - 1, d, h).getTime();
const conv = (id: string, startedAt: number, title = `question ${id}`): Conversation => ({ id, pcId: 'pc-1', startedAt, title, turns: [] });
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

test('save adds or replaces, newest first, at most 100, and keeps it on the phone', async () => {
  const { kv } = memory();
  await conversations.init(kv);
  await conversations.save(conv('a', 1000));
  await conversations.save(conv('b', 3000));
  await conversations.save(conv('c', 2000));
  expect(conversations.list().map((c) => c.id)).toEqual(['b', 'c', 'a']);

  // Saving one again replaces it (its turns grew), and it keeps its place by time.
  await conversations.save({ ...conv('a', 1000), title: 'changed' });
  expect(conversations.list().map((c) => c.id)).toEqual(['b', 'c', 'a']);
  expect((await conversations.load('a'))?.title).toBe('changed');

  for (let i = 0; i < 110; i++) await conversations.save(conv(`n${i}`, 10000 + i));
  expect(conversations.list()).toHaveLength(maxConversations);
  expect(maxConversations).toBe(100);
  expect(conversations.list()[0].id).toBe('n109');
  expect(await conversations.load('a')).toBeUndefined(); // the oldest went, from the phone too

  await conversations.remove('n109');
  expect(conversations.list()[0].id).toBe('n108');
  expect(await conversations.load('n109')).toBeUndefined();
});

test('after the app starts again, init brings the kept heads back; a whole conversation is read when opened', async () => {
  const { kv } = memory();
  await conversations.init(kv);
  const turn = { id: 't1', question: 'q', req: { mode: 'ask', prompt: 'q', tier: 'quick', agents: [{ agent: 'claude' }], clientRunId: 't1' }, agents: ['claude'] };
  await conversations.save({ ...conv('a', 1000, 'first'), turns: [turn as never] });
  await conversations.save(conv('b', 2000, 'second'));

  conversations.reset(); // the app is closed
  expect(conversations.list()).toEqual([]);
  await conversations.init(kv);
  expect(conversations.list()).toEqual([
    { id: 'b', startedAt: 2000, title: 'second' },
    { id: 'a', startedAt: 1000, title: 'first' },
  ]);
  expect((await conversations.load('a'))?.turns).toEqual([turn]);
});

test('removing the current conversation drops it: the Ask screen takes it once, and a late save does not bring it back', async () => {
  const { kv } = memory();
  await conversations.init(kv);
  await conversations.save(conv('a', 1000));
  conversations.setCurrent({ id: 'a', startedAt: 1000, title: 'question a' });
  await conversations.remove('a');
  expect(conversations.current()).toBeNull();
  expect(conversations.takeDropped()).toBe('a');
  expect(conversations.takeDropped()).toBeNull();
  await conversations.save(conv('a', 1000)); // a save still on its way
  expect(conversations.list()).toEqual([]);
  expect(await conversations.load('a')).toBeUndefined();
});

test('remove all empties the phone and drops the current one', async () => {
  const { mem, kv } = memory();
  await conversations.init(kv);
  await conversations.save(conv('a', 1000));
  await conversations.save(conv('b', 2000));
  mem.set('conv.v1.orphan', '{}'); // written once without its list entry
  conversations.setCurrent({ id: 'c', startedAt: 3000, title: 'now' });
  await conversations.removeAll();
  expect(conversations.list()).toEqual([]);
  expect(conversations.takeDropped()).toBe('c');
  expect([...mem.keys()]).toEqual(['index.v1']);
  expect(mem.get('index.v1')).toBe('[]');
});

test('the drawer groups by day: today, yesterday, then the date; the current one first', () => {
  const now = new Date(2026, 10, 2, 15, 0); // 2 Nov 2026, 15:00
  const list = [conv('y1', at(2026, 11, 1, 9)), conv('t1', at(2026, 11, 2, 8)), conv('o1', at(2026, 10, 29, 20)), conv('t2', at(2026, 11, 2, 11))];
  const current = { id: 'cur', startedAt: at(2026, 11, 2, 7), title: 'now asking' };

  const ko = dayGroups({ list, current, now, lang: 'ko' });
  expect(ko.map((g) => g.label)).toEqual(['오늘', '어제', '10월 29일']);
  expect(ko[0].items).toEqual([
    { id: 'cur', title: 'now asking', current: true },
    { id: 't2', title: 'question t2', current: false },
    { id: 't1', title: 'question t1', current: false },
  ]);
  expect(ko[1].items.map((x) => x.id)).toEqual(['y1']);
  expect(ko[2].items.map((x) => x.id)).toEqual(['o1']);

  const en = dayGroups({ list, current, now, lang: 'en' });
  expect(en.map((g) => g.label)).toEqual(['Today', 'Yesterday', '10/29']);

  // A conversation that is both saved and current shows once, as current.
  const again = dayGroups({ list: [...list, conv('cur', current.startedAt)], current, now, lang: 'en' });
  expect(again[0].items.filter((x) => x.id === 'cur')).toEqual([{ id: 'cur', title: 'now asking', current: true }]);

  expect(dayGroups({ list: [], current: null, now, lang: 'en' })).toEqual([]);
});

test('yesterday across a month and a year', () => {
  const now = new Date(2027, 0, 1, 0, 30);
  const groups = dayGroups({ list: [conv('a', at(2026, 12, 31, 23)), conv('b', at(2026, 12, 30, 1))], current: null, now, lang: 'ko' });
  expect(groups.map((g) => g.label)).toEqual(['어제', '12월 30일']);
});

test('recent questions are distinct first questions, newest first, the current one too', async () => {
  expect(recentQuestions()).toEqual([]);
  await conversations.save(conv('a', 1000, '서울 인구는?'));
  await conversations.save(conv('b', 2000, '  서울 인구는?  '));
  await conversations.save(conv('c', 3000, '커피는 하루에 몇 잔?'));
  await conversations.save(conv('d', 4000, '에베레스트 높이는?'));
  expect(recentQuestions()).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?', '서울 인구는?']);
  expect(recentQuestions(2)).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?']);
  // Room for a fourth: the same question asked twice is still one (TEST-2; the screen keys rows by question).
  expect(recentQuestions(4)).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?', '서울 인구는?']);
  conversations.setCurrent({ id: 'e', startedAt: 5000, title: '전기차 배터리 수명은?' });
  expect(recentQuestions(1)).toEqual(['전기차 배터리 수명은?']);
});

test('an open request is taken once', async () => {
  await conversations.save(conv('a', 1000));
  expect(conversations.takeOpenRequest()).toBeNull();
  conversations.requestOpen('a');
  expect(conversations.takeOpenRequest()).toBe('a');
  expect(conversations.takeOpenRequest()).toBeNull();
  // Removing the conversation drops a request for it.
  conversations.requestOpen('a');
  await conversations.remove('a');
  expect(conversations.takeOpenRequest()).toBeNull();
});

test('screens see the list, the current head and the request change', async () => {
  const { result } = await renderHook(() => useConversations());
  expect(result.current).toEqual({ list: [], current: null, openRequest: null, dropped: null });
  await act(async () => conversations.save(conv('a', 1000)));
  expect(result.current.list.map((c) => c.id)).toEqual(['a']);
  await act(async () => conversations.setCurrent({ id: 'b', startedAt: 2000, title: 'q' }));
  expect(result.current.current).toEqual({ id: 'b', startedAt: 2000, title: 'q' });
  await act(async () => conversations.requestOpen('a'));
  expect(result.current.openRequest).toBe('a');
  await act(async () => void conversations.takeOpenRequest());
  expect(result.current.openRequest).toBeNull();
  await act(async () => conversations.reset());
  expect(result.current).toEqual({ list: [], current: null, openRequest: null, dropped: null });
});
