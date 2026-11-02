// S5c: this run's questions for the drawer and '최근 질문', kept in memory
// only, at most 50, newest first; grouped by day for the drawer.

import { act, renderHook } from '@testing-library/react-native';

import { type Conversation, conversations, dayGroups, maxConversations, recentQuestions, useConversations } from './conversations';

const at = (y: number, m: number, d: number, h = 12) => new Date(y, m - 1, d, h).getTime();
const conv = (id: string, startedAt: number, title = `question ${id}`): Conversation => ({ id, pcId: 'pc-1', startedAt, title, turns: [] });

beforeEach(() => conversations.clear());

test('save adds or replaces, newest first, at most 50', () => {
  conversations.save(conv('a', 1000));
  conversations.save(conv('b', 3000));
  conversations.save(conv('c', 2000));
  expect(conversations.list().map((c) => c.id)).toEqual(['b', 'c', 'a']);

  // Saving one again replaces it (its turns grew), and it keeps its place by time.
  conversations.save({ ...conv('a', 1000), title: 'changed' });
  expect(conversations.list().map((c) => c.id)).toEqual(['b', 'c', 'a']);
  expect(conversations.get('a')?.title).toBe('changed');

  for (let i = 0; i < 60; i++) conversations.save(conv(`n${i}`, 10000 + i));
  expect(conversations.list()).toHaveLength(maxConversations);
  expect(maxConversations).toBe(50);
  expect(conversations.list()[0].id).toBe('n59');
  expect(conversations.get('a')).toBeUndefined(); // the oldest went

  conversations.remove('n59');
  expect(conversations.list()[0].id).toBe('n58');
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

test('recent questions are distinct first questions, newest first, the current one too', () => {
  expect(recentQuestions()).toEqual([]);
  conversations.save(conv('a', 1000, '서울 인구는?'));
  conversations.save(conv('b', 2000, '  서울 인구는?  '));
  conversations.save(conv('c', 3000, '커피는 하루에 몇 잔?'));
  conversations.save(conv('d', 4000, '에베레스트 높이는?'));
  expect(recentQuestions()).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?', '서울 인구는?']);
  expect(recentQuestions(2)).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?']);
  // Room for a fourth: the same question asked twice is still one (TEST-2; the screen keys rows by question).
  expect(recentQuestions(4)).toEqual(['에베레스트 높이는?', '커피는 하루에 몇 잔?', '서울 인구는?']);
  conversations.setCurrent({ id: 'e', startedAt: 5000, title: '전기차 배터리 수명은?' });
  expect(recentQuestions(1)).toEqual(['전기차 배터리 수명은?']);
});

test('an open request is taken once', () => {
  conversations.save(conv('a', 1000));
  expect(conversations.takeOpenRequest()).toBeNull();
  conversations.requestOpen('a');
  expect(conversations.takeOpenRequest()).toBe('a');
  expect(conversations.takeOpenRequest()).toBeNull();
  // Removing the conversation drops a request for it.
  conversations.requestOpen('a');
  conversations.remove('a');
  expect(conversations.takeOpenRequest()).toBeNull();
});

test('screens see the list, the current head and the request change', async () => {
  const { result } = await renderHook(() => useConversations());
  expect(result.current).toEqual({ list: [], current: null, openRequest: null });
  await act(async () => conversations.save(conv('a', 1000)));
  expect(result.current.list.map((c) => c.id)).toEqual(['a']);
  await act(async () => conversations.setCurrent({ id: 'b', startedAt: 2000, title: 'q' }));
  expect(result.current.current).toEqual({ id: 'b', startedAt: 2000, title: 'q' });
  await act(async () => conversations.requestOpen('a'));
  expect(result.current.openRequest).toBe('a');
  await act(async () => void conversations.takeOpenRequest());
  expect(result.current.openRequest).toBeNull();
  await act(async () => conversations.clear());
  expect(result.current).toEqual({ list: [], current: null, openRequest: null });
});
