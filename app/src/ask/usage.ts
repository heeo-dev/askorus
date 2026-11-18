// What is left of each AI's plan (#20, decisions 9-21): the bars of the
// top bar's usage button, its sheet and the Manage AIs rows read it from
// /v1/info's usage groups. An AI's bar is its tightest window (the least
// left); 'low' is under 20%; a window the AI says is used up, or with
// nothing left, is 'out'; an AI that reports nothing is 'unknown' (never
// shown as 0%).

import type { AdapterInfo, Info } from '@/api/types';

export type UsageGroup = NonNullable<AdapterInfo['usage']>[number];
export type UsageState = 'ok' | 'low' | 'out' | 'unknown';

/** lowBelow: less left than this is 'low' (decision 19). */
export const lowBelow = 0.2;

/** percentLeft is a share left as a whole percent; anything above zero shows at least 1%. */
export function percentLeft(left: number): number {
  const v = Math.min(1, Math.max(0, left));
  if (v <= 0) return 0;
  return Math.max(1, Math.round(v * 100));
}

/** used: the window is used up (the AI says so, or nothing is left). */
export const used = (g: UsageGroup) => !!g.limited || g.left <= 0;

/** stateOf is one window's state. */
export function stateOf(g: UsageGroup): UsageState {
  if (used(g)) return 'out';
  return g.left < lowBelow ? 'low' : 'ok';
}

/** tightest is the window with the least left, a used-up one first; undefined with none. */
export function tightest(groups: UsageGroup[] | undefined): UsageGroup | undefined {
  let best: UsageGroup | undefined;
  for (const g of groups ?? []) {
    if (!best || (used(g) && !used(best)) || (used(g) === used(best) && g.left < best.left)) best = g;
  }
  return best;
}

export type AIUsage = {
  agent: string;
  groups: UsageGroup[];
  /** The bar's window: the least left. */
  tight?: UsageGroup;
  state: UsageState;
};

export function aiUsage(a: AdapterInfo): AIUsage {
  const groups = a.usage ?? [];
  const tight = tightest(groups);
  return { agent: a.agent, groups, tight, state: tight ? stateOf(tight) : 'unknown' };
}

/** canAsk: the AI is installed and answers questions. */
const canAsk = (a: AdapterInfo) => a.available && a.capabilities.includes('ask');

export type UsageSummary =
  | { kind: 'loading' }
  | { kind: 'off'; off: string[] }
  | { kind: 'ready'; ais: AIUsage[]; off: string[] };

/**
 * summarize gives the bars of the AIs turned on, in the PC's order (the
 * composer's), and the AIs turned off (decision 17). No info yet: loading.
 */
export function summarize(info: Info | null, offList: string[]): UsageSummary {
  if (!info) return { kind: 'loading' };
  const askers = info.agents.filter(canAsk);
  const off = askers.filter((a) => offList.includes(a.agent)).map((a) => a.agent);
  const on = askers.filter((a) => !offList.includes(a.agent));
  if (on.length === 0) return { kind: 'off', off };
  return { kind: 'ready', ais: on.map(aiUsage), off };
}

/** worst is the state the whole button shows: a used-up AI, then a low one, then any known, else unknown. */
export function worst(ais: AIUsage[]): UsageState {
  for (const s of ['out', 'low', 'ok'] as const) if (ais.some((a) => a.state === s)) return s;
  return 'unknown';
}

/** lowest is the AI with the tightest window of all (a used-up one first), for the summary line. */
export function lowest(ais: AIUsage[]): AIUsage | undefined {
  let best: AIUsage | undefined;
  for (const a of ais) {
    if (!a.tight) continue;
    if (!best?.tight || (used(a.tight) && !used(best.tight)) || (used(a.tight) === used(best.tight) && a.tight.left < best.tight.left)) best = a;
  }
  return best;
}
