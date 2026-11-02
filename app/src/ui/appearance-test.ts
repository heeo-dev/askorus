// S5c: the theme picked in Settings is kept on the phone under its own key;
// nothing saved, or anything unreadable, follows the phone (system).

import { loadThemeChoice, saveThemeChoice } from './appearance';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

test('nothing saved follows the phone', async () => {
  const { kv } = memory();
  expect(await loadThemeChoice(kv)).toBe('system');
});

test('each choice survives a round trip, under ui.v1 apart from the ask prefs', async () => {
  const { mem, kv } = memory();
  for (const c of ['dark', 'light', 'system'] as const) {
    await saveThemeChoice(c, kv);
    expect(await loadThemeChoice(kv)).toBe(c);
  }
  expect([...mem.keys()]).toEqual(['ui.v1']);
  expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'system' });
});

test('bad data follows the phone', async () => {
  const { mem, kv } = memory();
  for (const raw of ['not json', '[]', 'null', '"dark"', '{"theme":"sepia"}', '{"theme":1}', '{}']) {
    mem.set('ui.v1', raw);
    expect([raw, await loadThemeChoice(kv)]).toEqual([raw, 'system']);
  }
  const broken = { get: async () => Promise.reject(new Error('keystore')), set: async () => {}, remove: async () => {} };
  expect(await loadThemeChoice(broken)).toBe('system');
});
