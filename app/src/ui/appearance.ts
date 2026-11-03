// The look picked in Settings: the theme (S5c: follow the phone (system),
// or always dark or light) and whether AIs have their own colours (#6, on
// unless turned off). Saved on the phone under its own key, apart from the
// ask prefs; anything unreadable counts as the default.

import { type KeyValue, secureKeyValue } from '@/connections/store';

import type { ThemeChoice } from './theme';

const key = 'ui.v1';
const choices: readonly string[] = ['system', 'dark', 'light'];

export type Appearance = { theme: ThemeChoice; aiColours: boolean };
export const defaultAppearance: Appearance = { theme: 'system', aiColours: true };

export async function loadAppearance(kv: KeyValue = secureKeyValue): Promise<Appearance> {
  try {
    const v = JSON.parse((await kv.get(key)) ?? '{}');
    if (!v || typeof v !== 'object') return defaultAppearance;
    return {
      theme: choices.includes(v.theme) ? v.theme : defaultAppearance.theme,
      aiColours: typeof v.aiColours === 'boolean' ? v.aiColours : defaultAppearance.aiColours,
    };
  } catch {
    return defaultAppearance;
  }
}

export async function saveAppearance(a: Appearance, kv: KeyValue = secureKeyValue): Promise<void> {
  await kv.set(key, JSON.stringify({ theme: a.theme, aiColours: a.aiColours }));
}
