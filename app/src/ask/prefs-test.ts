import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, synthWriters } from './prefs';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

const defaults = { tier: 'standard', off: [], models: {}, pc: '', compare: true, synth: true, synthAgent: 'claude', refine: true };

test('ask prefs survive a round trip, compare and the combined answer off included', async () => {
  const { kv } = memory();
  expect(await loadAskPrefs(kv)).toEqual(defaults);
  const saved = { tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1', compare: false, synth: false, synthAgent: 'codex', refine: false } as const;
  await saveAskPrefs({ ...saved, off: [...saved.off], models: { ...saved.models } }, kv);
  expect(await loadAskPrefs(kv)).toEqual(saved);
  await saveAskPrefs({ ...defaultAskPrefs, compare: true }, kv);
  expect((await loadAskPrefs(kv)).compare).toBe(true);
  for (const synthAgent of synthWriters) {
    await saveAskPrefs({ ...defaultAskPrefs, synthAgent }, kv);
    expect((await loadAskPrefs(kv)).synthAgent).toBe(synthAgent);
  }
});

// P2-3: prefs saved before compare existed keep auto compare on.
test('a missing or odd compare is on', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs.compare).toBe(true);
  mem.set('ask.v1', JSON.stringify({ tier: 'quick', off: [], models: {}, pc: 'pc-1' }));
  expect(await loadAskPrefs(kv)).toEqual({ ...defaults, tier: 'quick', pc: 'pc-1' });
  for (const odd of ['false', 0, null, {}]) {
    mem.set('ask.v1', JSON.stringify({ compare: odd }));
    expect((await loadAskPrefs(kv)).compare).toBe(true);
  }
});

// S5b: prefs saved before the combined answer existed write one, by Claude.
test('a missing or odd combined-answer setting is on, written by Claude', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs).toMatchObject({ synth: true, synthAgent: 'claude' });
  mem.set('ask.v1', JSON.stringify({ tier: 'deep', off: [], models: {}, pc: 'pc-1', compare: false }));
  expect(await loadAskPrefs(kv)).toEqual({ ...defaults, tier: 'deep', pc: 'pc-1', compare: false });
  for (const odd of ['false', 0, null, {}]) {
    mem.set('ask.v1', JSON.stringify({ synth: odd }));
    expect((await loadAskPrefs(kv)).synth).toBe(true);
  }
  for (const odd of ['', 'gemini', 'Claude', 7, null, ['codex'], { agent: 'codex' }]) {
    mem.set('ask.v1', JSON.stringify({ synthAgent: odd }));
    expect((await loadAskPrefs(kv)).synthAgent).toBe('claude');
  }
});

test('bad values still fall back', async () => {
  const { mem, kv } = memory();
  mem.set('ask.v1', JSON.stringify({ tier: 'max', off: 'x', models: { a: 3, b: '' }, pc: 7, compare: false, synth: 'no', synthAgent: 'x' }));
  expect(await loadAskPrefs(kv)).toEqual({ ...defaults, compare: false });
  mem.set('ask.v1', '{not json');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
  mem.set('ask.v1', 'null');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
});

// Milestone C: refining is on unless turned off; prefs saved before it refine.
test('a missing or odd refine setting is on', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs.refine).toBe(true);
  for (const odd of [undefined, 'false', 0, null]) {
    mem.set('ask.v1', JSON.stringify({ tier: 'quick', refine: odd }));
    expect((await loadAskPrefs(kv)).refine).toBe(true);
  }
  mem.set('ask.v1', JSON.stringify({ refine: false }));
  expect((await loadAskPrefs(kv)).refine).toBe(false);
});
