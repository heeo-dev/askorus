import { defaultAskPrefs, loadAskPrefs, saveAskPrefs, synthWriters } from './prefs';

const memory = () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  return { mem, kv };
};

const defaults = { tier: 'standard', off: [], models: {}, efforts: {}, pc: '', synth: true, synthAgent: 'claude', synthModels: {}, refine: false, debateNoticed: false, carryRecord: true, recordNoticed: false };

test('ask prefs survive a round trip, the combined answer off included', async () => {
  const { kv } = memory();
  expect(await loadAskPrefs(kv)).toEqual(defaults);
  const saved = { tier: 'deep', off: ['codex'], models: { claude: 'opus' }, efforts: { claude: 'high' }, pc: 'pc-1', synth: false, synthAgent: 'codex', synthModels: { codex: { model: 'gpt-6.1-sol', effort: 'high' } }, refine: true, debateNoticed: true, carryRecord: false, recordNoticed: true } as const;
  await saveAskPrefs({ ...saved, off: [...saved.off], models: { ...saved.models }, efforts: { ...saved.efforts }, synthModels: { codex: { ...saved.synthModels.codex } } }, kv);
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

// 11-06: the effort picked with an AI's model; prefs saved before it pick
// none, and an effort without its model, or not a word, is dropped.
test('an effort is kept only with its model', async () => {
  const { mem, kv } = memory();
  mem.set('ask.v1', JSON.stringify({ models: { opencode: 'opencode-go/deepseek-v4.1-flash' } }));
  expect(await loadAskPrefs(kv)).toMatchObject({ models: { opencode: 'opencode-go/deepseek-v4.1-flash' }, efforts: {} });
  mem.set('ask.v1', JSON.stringify({ models: { opencode: 'flash', claude: 'opus' }, efforts: { opencode: 'low', codex: 'high', claude: 3 } }));
  expect((await loadAskPrefs(kv)).efforts).toEqual({ opencode: 'low' });
});

// 11-06: the moderator model set per AI; a pick without a model is dropped.
test('moderator models keep only picks with a model', async () => {
  const { mem, kv } = memory();
  mem.set(
    'ask.v1',
    JSON.stringify({ synthModels: { claude: { model: 'opus', effort: 'high' }, codex: { model: 'gpt-6-luna' }, opencode: { effort: 'max' }, x: 'opus', y: { model: 3 } } }),
  );
  expect((await loadAskPrefs(kv)).synthModels).toEqual({ claude: { model: 'opus', effort: 'high' }, codex: { model: 'gpt-6-luna' } });
});

// #29: a follow-up carries the record unless turned off; the notice is seen once.
test('a missing or odd record setting carries the record, notice not seen', async () => {
  const { mem, kv } = memory();
  expect(defaultAskPrefs).toMatchObject({ carryRecord: true, recordNoticed: false });
  for (const odd of ['false', 0, null, {}]) {
    mem.set('ask.v1', JSON.stringify({ carryRecord: odd, recordNoticed: odd }));
    expect(await loadAskPrefs(kv)).toMatchObject({ carryRecord: true, recordNoticed: false });
  }
  mem.set('ask.v1', JSON.stringify({ carryRecord: false, recordNoticed: true }));
  expect(await loadAskPrefs(kv)).toMatchObject({ carryRecord: false, recordNoticed: true });
});
