// What the next ask starts with, remembered on the phone (P2-2): the tier
// (also set in Settings as the default), the AIs turned off, a model the
// person picked for an AI (empty: by tier), and the PC that answers. synth
// and synthAgent (S5b): once the answers are in, the moderator writes a
// combined answer, and which AI it is; prefs saved before them keep it on,
// written by Claude. refine (milestone C): the moderator refines each
// question first; off unless turned on, and only used while the feature is
// on (features.ts, off since 2026-11-04). The compare switch of P2-3 went
// with judging (C6d): a value saved for it is no longer read.
// debateNoticed (P2-5): the person has seen, once, what a debate sends
// where.

import { tiers, type Tier } from '@/api/types';
import { type KeyValue, secureKeyValue } from '@/connections/store';

/**
 * The AIs Settings offers as the writer. Settings has no PC info, so it is
 * a fixed list; a PC without the picked AI writes with the first asked AI
 * that can (the agent picks).
 */
export const synthWriters = ['claude', 'codex', 'opencode'] as const;

export type AskPrefs = {
  tier: Tier;
  off: string[];
  models: Record<string, string>;
  pc: string;
  synth: boolean;
  synthAgent: string;
  refine: boolean;
  /** The person has seen, once, what a debate sends where (P2-5). */
  debateNoticed: boolean;
};

const key = 'ask.v1';
export const defaultAskPrefs: AskPrefs = { tier: 'standard', off: [], models: {}, pc: '', synth: true, synthAgent: 'claude', refine: false, debateNoticed: false };

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
      synth: typeof v.synth === 'boolean' ? v.synth : true,
      synthAgent: (synthWriters as readonly unknown[]).includes(v.synthAgent) ? v.synthAgent : 'claude',
      refine: typeof v.refine === 'boolean' ? v.refine : false,
      debateNoticed: v.debateNoticed === true,
    };
  } catch {
    return defaultAskPrefs;
  }
}

export async function saveAskPrefs(p: AskPrefs, kv: KeyValue = secureKeyValue): Promise<void> {
  await kv.set(key, JSON.stringify(p));
}
