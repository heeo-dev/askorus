// What the next ask starts with, remembered on the phone (P2-2): the tier
// (also set in Settings as the default), the AIs turned off, a model the
// person picked for an AI (empty: by tier), and the PC that answers. compare
// (P2-3): the answers are compared once they are in; prefs saved before it
// existed keep it on.

import { tiers, type Tier } from '@/api/types';
import { type KeyValue, secureKeyValue } from '@/connections/store';

export type AskPrefs = { tier: Tier; off: string[]; models: Record<string, string>; pc: string; compare: boolean };

const key = 'ask.v1';
export const defaultAskPrefs: AskPrefs = { tier: 'standard', off: [], models: {}, pc: '', compare: true };

export async function loadAskPrefs(kv: KeyValue = secureKeyValue): Promise<AskPrefs> {
  try {
    const v = JSON.parse((await kv.get(key)) ?? '{}');
    const models: Record<string, string> = {};
    if (v.models && typeof v.models === 'object') {
      for (const [a, m] of Object.entries(v.models)) if (typeof m === 'string' && m) models[a] = m;
    }
    return {
      tier: (tiers as readonly string[]).includes(v.tier) ? v.tier : 'standard',
      off: Array.isArray(v.off) ? v.off.filter((x: unknown) => typeof x === 'string') : [],
      models,
      pc: typeof v.pc === 'string' ? v.pc : '',
      compare: typeof v.compare === 'boolean' ? v.compare : true,
    };
  } catch {
    return defaultAskPrefs;
  }
}

export async function saveAskPrefs(p: AskPrefs, kv: KeyValue = secureKeyValue): Promise<void> {
  await kv.set(key, JSON.stringify(p));
}
