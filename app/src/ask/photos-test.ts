import type { Info } from '@/api/types';

import { askModel, byPhotos, photoLimit, scaled, seesPhotos } from './photos';

const tiers = (a: string) => ({ quick: { model: `${a}-quick` }, standard: { model: `${a}-standard` }, deep: { model: `${a}-deep` } });

function info(over: Partial<Info> = {}): Info {
  return {
    name: 'askorus-agent',
    version: '0.4.0',
    apiVersion: 1,
    hostname: 'PC',
    os: 'windows',
    capabilities: ['ask'],
    reserved: [],
    attachments: { maxCount: 4, maxBytes: 4 << 20, types: ['image/jpeg', 'image/png', 'image/webp'] },
    agents: [
      { agent: 'claude', available: true, capabilities: ['ask'], ask: { tiers: tiers('claude'), images: { all: true } } },
      { agent: 'codex', available: true, capabilities: ['ask'], ask: { tiers: tiers('codex'), images: { all: true } } },
      { agent: 'opencode', available: true, capabilities: ['ask'], ask: { tiers: tiers('opencode'), images: { models: ['opencode-quick', 'opencode-standard'] } } },
      { agent: 'agy', available: true, capabilities: ['ask'], ask: { tiers: tiers('agy') } },
    ],
    ...over,
  };
}

test('a PC says how many photos a question may carry; an older agent takes none', () => {
  expect(photoLimit(info())).toBe(4);
  expect(photoLimit(info({ attachments: undefined }))).toBe(0);
  expect(photoLimit(null)).toBe(0);
  expect(photoLimit(info({ attachments: { maxCount: 0, maxBytes: 1, types: [] } }))).toBe(0);
  expect(photoLimit(info({ attachments: { maxCount: 2.9, maxBytes: 1, types: [] } }))).toBe(2);
});

test('an AI sees photos with all its models, with the ones the PC lists, or with none', () => {
  const i = info();
  expect(seesPhotos(i, 'claude', 'anything')).toBe(true);
  expect(seesPhotos(i, 'claude', undefined)).toBe(true);
  expect(seesPhotos(i, 'opencode', 'opencode-quick')).toBe(true);
  // A model's '#variant' (OpenCode's effort) is not part of its name.
  expect(seesPhotos(i, 'opencode', 'opencode-standard#high')).toBe(true);
  expect(seesPhotos(i, 'opencode', 'opencode-deep')).toBe(false);
  expect(seesPhotos(i, 'opencode', 'opencode-quick-2')).toBe(false);
  expect(seesPhotos(i, 'opencode', undefined)).toBe(false);
  // The PC says nothing about this AI's photos, or does not know the AI at all.
  expect(seesPhotos(i, 'agy', 'agy-quick')).toBe(false);
  expect(seesPhotos(i, 'gemini', 'x')).toBe(false);
});

test('an AI answers with the model picked for it, else with its tier model', () => {
  const i = info();
  expect(askModel(i, 'opencode', 'deep', {})).toBe('opencode-deep');
  expect(askModel(i, 'opencode', 'deep', { opencode: 'opencode-quick#low' })).toBe('opencode-quick#low');
  expect(askModel(i, 'gemini', 'deep', {})).toBeUndefined();
});

test('the AIs of a question with photos are split into those that see them and those that sit out', () => {
  const i = info();
  const all = ['claude', 'codex', 'opencode'];
  expect(byPhotos(i, all, 'standard', {})).toEqual({ sees: all, blind: [] });
  expect(byPhotos(i, all, 'deep', {})).toEqual({ sees: ['claude', 'codex'], blind: ['opencode'] });
  // Its own model, picked in the AI picker, counts whatever the tier.
  expect(byPhotos(i, all, 'deep', { opencode: 'opencode-quick' })).toEqual({ sees: all, blind: [] });
  expect(byPhotos(i, all, 'quick', { opencode: 'opencode-deep' })).toEqual({ sees: ['claude', 'codex'], blind: ['opencode'] });
  expect(byPhotos(i, ['opencode', 'agy'], 'deep', {})).toEqual({ sees: [], blind: ['opencode', 'agy'] });
  expect(byPhotos(info({ agents: [] }), all, 'quick', {})).toEqual({ sees: [], blind: all });
});

// R-130 (#20): Antigravity sees photos only in a new conversation; a
// follow-up that carries its conversation on leaves it out.
test('an AI that sees photos only in a new conversation sits out a follow-up that carries one on', () => {
  const base = info();
  const i = { ...base, agents: base.agents.map((a) => (a.agent === 'agy' ? { ...a, ask: { ...a.ask!, images: { all: true, newOnly: true } } } : a)) };
  expect(byPhotos(i, ['claude', 'agy'], 'standard', {})).toEqual({ sees: ['claude', 'agy'], blind: [] });
  expect(byPhotos(i, ['claude', 'agy'], 'standard', {}, { claude: 'claude:a', agy: 'agy:b' })).toEqual({ sees: ['claude'], blind: ['agy'] });
  // Carrying on Claude's conversation alone leaves Antigravity a new one.
  expect(byPhotos(i, ['claude', 'agy'], 'standard', {}, { claude: 'claude:a' })).toEqual({ sees: ['claude', 'agy'], blind: [] });
});

test('a photo is scaled down to its long edge and never up', () => {
  expect(scaled(4000, 3000)).toEqual({ width: 2048, height: 1536 });
  expect(scaled(3000, 4000)).toEqual({ width: 1536, height: 2048 });
  expect(scaled(1080, 1920)).toEqual({ width: 1080, height: 1920 });
  expect(scaled(1080, 2340)).toEqual({ width: 945, height: 2048 });
  expect(scaled(2048, 2048)).toEqual({ width: 2048, height: 2048 });
  expect(scaled(100000, 10)).toEqual({ width: 2048, height: 1 });
  expect(scaled(800, 600, 400)).toEqual({ width: 400, height: 300 });
});
