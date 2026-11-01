import { defaultAskPrefs, loadAskPrefs, saveAskPrefs } from './prefs';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

test('ask prefs survive a round trip, compare off included', async () => {
  const { kv } = memory();
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'standard', off: [], models: {}, pc: '', compare: true });
  await saveAskPrefs({ tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1', compare: false }, kv);
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1', compare: false });
  await saveAskPrefs({ ...defaultAskPrefs, compare: true }, kv);
  expect((await loadAskPrefs(kv)).compare).toBe(true);
});

// P2-3: prefs saved before compare existed keep auto compare on.
test('a missing or odd compare is on', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs.compare).toBe(true);
  mem.set('ask.v1', JSON.stringify({ tier: 'quick', off: [], models: {}, pc: 'pc-1' }));
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'quick', off: [], models: {}, pc: 'pc-1', compare: true });
  for (const odd of ['false', 0, null, {}]) {
    mem.set('ask.v1', JSON.stringify({ compare: odd }));
    expect((await loadAskPrefs(kv)).compare).toBe(true);
  }
});

test('bad values still fall back', async () => {
  const { mem, kv } = memory();
  mem.set('ask.v1', JSON.stringify({ tier: 'max', off: 'x', models: { a: 3, b: '' }, pc: 7, compare: false }));
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'standard', off: [], models: {}, pc: '', compare: false });
  mem.set('ask.v1', '{not json');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
  mem.set('ask.v1', 'null');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
});
