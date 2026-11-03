// S5c and #6: the look picked in Settings (the theme, and whether AIs have
// their own colours) is kept on the phone under its own key; nothing saved,
// or anything unreadable, follows the phone with AI colours on.

import { defaultAppearance, loadAppearance, saveAppearance } from './appearance';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

test('nothing saved follows the phone, with AI colours on', async () => {
  const { kv } = memory();
  expect(await loadAppearance(kv)).toEqual({ theme: 'system', aiColours: true });
  expect(defaultAppearance).toEqual({ theme: 'system', aiColours: true });
});

test('each look survives a round trip, under ui.v1 apart from the ask prefs', async () => {
  const { mem, kv } = memory();
  for (const theme of ['dark', 'light', 'system'] as const) {
    for (const aiColours of [false, true]) {
      await saveAppearance({ theme, aiColours }, kv);
      expect(await loadAppearance(kv)).toEqual({ theme, aiColours });
    }
  }
  expect([...mem.keys()]).toEqual(['ui.v1']);
  expect(JSON.parse(mem.get('ui.v1') ?? '')).toEqual({ theme: 'system', aiColours: true });
});

test('a theme saved before AI colours (S5c) keeps it, with AI colours on', async () => {
  const { mem, kv } = memory();
  mem.set('ui.v1', '{"theme":"dark"}');
  expect(await loadAppearance(kv)).toEqual({ theme: 'dark', aiColours: true });
});

test('bad data counts as the default, field by field', async () => {
  const { mem, kv } = memory();
  for (const raw of ['not json', '[]', 'null', '"dark"', '{"theme":"sepia"}', '{"theme":1}', '{}', '{"aiColours":"no"}', '{"aiColours":0}']) {
    mem.set('ui.v1', raw);
    expect([raw, await loadAppearance(kv)]).toEqual([raw, { theme: 'system', aiColours: true }]);
  }
  mem.set('ui.v1', '{"theme":"sepia","aiColours":false}');
  expect(await loadAppearance(kv)).toEqual({ theme: 'system', aiColours: false });
  const broken = { get: async () => Promise.reject(new Error('keystore')), set: async () => {}, remove: async () => {} };
  expect(await loadAppearance(broken)).toEqual({ theme: 'system', aiColours: true });
});
