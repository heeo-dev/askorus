import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, synthWriters } from './prefs';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

const defaults = { tier: 'standard', off: [], models: {}, pc: '', synth: true, synthAgent: 'claude', refine: false, debateNoticed: false };

test('ask prefs survive a round trip, the combined answer off included', async () => {
  const { kv } = memory();
  expect(await loadAskPrefs(kv)).toEqual(defaults);
  const saved = { tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1', synth: false, synthAgent: 'codex', refine: true, debateNoticed: true } as const;
  await saveAskPrefs({ ...saved, off: [...saved.off], models: { ...saved.models } }, kv);
  expect(await loadAskPrefs(kv)).toEqual(saved);
  for (const synthAgent of synthWriters) {
    await saveAskPrefs({ ...defaultAskPrefs, synthAgent }, kv);
    expect((await loadAskPrefs(kv)).synthAgent).toBe(synthAgent);
  }
});

// C6d: the compare switch of P2-3 went with judging; a value saved for it is not read.
test('prefs saved with the old compare switch load without it', async () => {
  const { mem, kv } = memory();
  mem.set('ask.v1', JSON.stringify({ tier: 'quick', off: [], models: {}, pc: 'pc-1', compare: false, synth: false }));
  expect(await loadAskPrefs(kv)).toEqual({ ...defaults, tier: 'quick', pc: 'pc-1', synth: false });
  expect('compare' in (await loadAskPrefs(kv))).toBe(false);
});

// S5b: prefs saved before the combined answer existed write one, by Claude.
test('a missing or odd combined-answer setting is on, written by Claude', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs).toMatchObject({ synth: true, synthAgent: 'claude' });
  mem.set('ask.v1', JSON.stringify({ tier: 'deep', off: [], models: {}, pc: 'pc-1' }));
  expect(await loadAskPrefs(kv)).toEqual({ ...defaults, tier: 'deep', pc: 'pc-1' });
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
  mem.set('ask.v1', JSON.stringify({ tier: 'max', off: 'x', models: { a: 3, b: '' }, pc: 7, synth: 'no', synthAgent: 'x' }));
  expect(await loadAskPrefs(kv)).toEqual(defaults);
  mem.set('ask.v1', '{not json');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
  mem.set('ask.v1', 'null');
  expect(await loadAskPrefs(kv)).toEqual(defaultAskPrefs);
});

// Milestone C: refining is off unless turned on (since 2026-11-04, when
// the feature itself went off); a saved choice is kept.
test('a missing or odd refine setting is off', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs.refine).toBe(false);
  for (const odd of [undefined, 'true', 1, null]) {
    mem.set('ask.v1', JSON.stringify({ tier: 'quick', refine: odd }));
    expect((await loadAskPrefs(kv)).refine).toBe(false);
  }
  mem.set('ask.v1', JSON.stringify({ refine: true }));
  expect((await loadAskPrefs(kv)).refine).toBe(true);
});

// P2-5: the debate's notice is shown once; prefs saved before it show it.
test('the debate notice is unseen until saved, and only true counts', async () => {
  const { mem, kv } = memory();
  mem.set('ask.v1', JSON.stringify({ tier: 'quick' }));
  expect((await loadAskPrefs(kv)).debateNoticed).toBe(false);
  for (const odd of ['true', 1, {}]) {
    mem.set('ask.v1', JSON.stringify({ debateNoticed: odd }));
    expect((await loadAskPrefs(kv)).debateNoticed).toBe(false);
  }
  await saveAskPrefs({ ...defaultAskPrefs, debateNoticed: true }, kv);
  expect((await loadAskPrefs(kv)).debateNoticed).toBe(true);
});
