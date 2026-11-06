// Which model and effort an AI answers with (11-06): labels, the tiers'
// pairs, the own pick's models and efforts.

import {
  effortChoices,
  effortOf,
  modelLabel,
  modelSpoken,
  pickModels,
  pickOf,
  presets,
  runsWith,
  samePick,
  withEffort,
  withModel,
} from './models';

// The PC's tiers as of 11-06.
const flash = 'opencode-go/deepseek-v4.1-flash';
const pro = 'opencode-go/deepseek-v4-pro';
const opencode = { quick: { model: flash, effort: 'low' }, standard: { model: flash, effort: 'high' }, deep: { model: pro, effort: 'high' } };
const opencodeEfforts = { [flash]: ['low', 'high', 'max'], [pro]: ['high', 'max'] };
const claude = { quick: { model: 'haiku', effort: 'medium' }, standard: { model: 'sonnet', effort: 'medium' }, deep: { model: 'opus', effort: 'medium' } };

test('a pick reads as its model and effort; a variant in the model is its effort', () => {
  expect(modelLabel({ model: flash, effort: 'low' })).toBe('deepseek-v4.1-flash · low');
  expect(modelSpoken({ model: flash, effort: 'low' })).toBe('deepseek-v4.1-flash, low');
  expect(modelLabel({ model: 'opus' })).toBe('opus');
  expect(effortOf({ model: `${pro}#max` })).toBe('max');
  expect(effortOf({ model: `${pro}#max`, effort: 'high' })).toBe('high');
  expect(samePick({ model: `${pro}#max` }, { model: pro, effort: 'max' })).toBe(true);
  expect(samePick({ model: flash, effort: 'low' }, { model: flash, effort: 'high' })).toBe(false);
});

// The bug: the model list showed OpenCode's Flash once, for low and high alike.
test('the tiers give one row per model and effort, with the tiers that use it', () => {
  expect(presets(opencode)).toEqual([
    { pick: { model: flash, effort: 'low' }, tiers: ['quick'] },
    { pick: { model: flash, effort: 'high' }, tiers: ['standard'] },
    { pick: { model: pro, effort: 'high' }, tiers: ['deep'] },
  ]);
  const same = { quick: { model: 'gpt-6-luna', effort: 'medium' }, standard: { model: 'gpt-6-luna', effort: 'medium' }, deep: { model: 'gpt-6.1-sol' } };
  expect(presets(same)).toEqual([
    { pick: { model: 'gpt-6-luna', effort: 'medium' }, tiers: ['quick', 'standard'] },
    { pick: { model: 'gpt-6.1-sol' }, tiers: ['deep'] },
  ]);
  expect(presets(claude).map((p) => modelLabel(p.pick))).toEqual(['haiku · medium', 'sonnet · medium', 'opus · medium']);
});

test("an own pick: the tiers' models once each, and each model's efforts", () => {
  expect(pickModels(opencode)).toEqual([flash, pro]);
  expect(effortChoices(opencode, opencodeEfforts, pro)).toEqual(['high', 'max']);
  // An older agent lists none: the efforts the tiers run the model with.
  expect(effortChoices(opencode, {}, flash)).toEqual(['low', 'high']);
  expect(effortChoices({ quick: { model: 'x' } }, undefined, 'x')).toEqual([]);
});

test('moving a pick to another model keeps its effort when that model takes it', () => {
  // Flash at low to Pro, which has no low: Pro's tier effort.
  expect(withModel(opencode, opencodeEfforts, { model: flash, effort: 'low' }, pro)).toEqual({ model: pro, effort: 'high' });
  expect(withModel(opencode, opencodeEfforts, { model: pro, effort: 'max' }, flash)).toEqual({ model: flash, effort: 'max' });
  // From by tier: the model's first tier.
  expect(withModel(opencode, opencodeEfforts, null, flash)).toEqual({ model: flash, effort: 'low' });
  expect(withModel({ quick: { model: 'x' } }, undefined, null, 'x')).toEqual({ model: 'x' });
  expect(withEffort({ model: pro, effort: 'high' }, flash, 'max')).toEqual({ model: pro, effort: 'max' });
  expect(withEffort(null, flash, 'max')).toEqual({ model: flash, effort: 'max' });
});

test("the person's pick, else the tier's", () => {
  const prefs = { models: { opencode: pro, claude: 'opus' }, efforts: { opencode: 'max' } };
  expect(pickOf(prefs, 'opencode')).toEqual({ model: pro, effort: 'max' });
  expect(pickOf(prefs, 'claude')).toEqual({ model: 'opus' });
  expect(pickOf(prefs, 'codex')).toBeNull();
  expect(runsWith(null, opencode, 'standard')).toEqual({ model: flash, effort: 'high' });
  expect(runsWith({ model: pro, effort: 'max' }, opencode, 'quick')).toEqual({ model: pro, effort: 'max' });
  expect(runsWith(null, {}, 'quick')).toBeNull();
});
