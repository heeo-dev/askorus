// What new sessions start with unless the person picks otherwise. Empty
// means "Claude's own default", so nothing is sent.

import { type KeyValue, secureKeyValue } from './store';

export const models = ['', 'opus', 'sonnet', 'haiku'] as const;
export const efforts = ['', 'low', 'medium', 'high', 'xhigh', 'max'] as const;

export type Prefs = { model: string; effort: string };

const key = 'prefs.v1';
const empty: Prefs = { model: '', effort: '' };

export async function loadPrefs(kv: KeyValue = secureKeyValue): Promise<Prefs> {
  try {
    const v = JSON.parse((await kv.get(key)) ?? '{}');
    return {
      model: typeof v.model === 'string' ? v.model : '',
      effort: (efforts as readonly string[]).includes(v.effort) ? v.effort : '',
    };
  } catch {
    return empty;
  }
}

export async function savePrefs(p: Prefs, kv: KeyValue = secureKeyValue): Promise<void> {
  await kv.set(key, JSON.stringify(p));
}
