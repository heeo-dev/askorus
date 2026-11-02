// The theme picked in Settings (S5c): follow the phone (system), or always
// dark or light. Saved on the phone under its own key, apart from the ask
// prefs; anything unreadable counts as system.

import { type KeyValue, secureKeyValue } from '@/connections/store';

import type { ThemeChoice } from './theme';

const key = 'ui.v1';
const choices: readonly string[] = ['system', 'dark', 'light'];

export async function loadThemeChoice(kv: KeyValue = secureKeyValue): Promise<ThemeChoice> {
  try {
    const v = JSON.parse((await kv.get(key)) ?? '{}');
    return v && typeof v === 'object' && choices.includes(v.theme) ? v.theme : 'system';
  } catch {
    return 'system';
  }
}

export async function saveThemeChoice(c: ThemeChoice, kv: KeyValue = secureKeyValue): Promise<void> {
  await kv.set(key, JSON.stringify({ theme: c }));
}
