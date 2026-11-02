// Screen text in Korean and English (N9). The agent sends codes only; the
// words are chosen here. Korean when the phone's first language is Korean,
// English otherwise.

import { getLocales } from 'expo-localization';

import { AgentError, NetworkError } from '../api/client';
import en from './en.json';
import ko from './ko.json';

export type MessageKey = keyof typeof ko;
export type Lang = 'ko' | 'en';

const tables: Record<Lang, Record<MessageKey, string>> = { ko, en };

export function pickLang(languageCode: string | null | undefined): Lang {
  return languageCode?.toLowerCase() === 'ko' ? 'ko' : 'en';
}

let lang: Lang = pickLang(getLocales()[0]?.languageCode);

export function currentLang(): Lang {
  return lang;
}

export function setLang(l: Lang) {
  lang = l;
}

/** t returns the text for key, with {name} filled from params. */
export function t(key: MessageKey, params: Record<string, string | number> = {}, l: Lang = lang): string {
  const text = tables[l][key] ?? tables.en[key] ?? key;
  return text.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m));
}

/** has reports whether key exists, for codes that come from the agent. */
export function has(key: string): key is MessageKey {
  return key in ko;
}

/**
 * label turns an agent code (state, badge, tool) into words; unknown codes
 * show as they are. An activity (S5c) shows only when it has words (now
 * only 'blocked', waiting for input): an unknown one shows nothing.
 */
export function label(prefix: 'state' | 'badge' | 'tool' | 'activity', code: string): string {
  const key = `${prefix}.${code}`;
  if (has(key)) return t(key);
  return prefix === 'activity' ? '' : code;
}

/** errorText explains a failed call in the person's language. */
export function errorText(e: unknown): string {
  if (e instanceof NetworkError) return t('error.network');
  if (e instanceof AgentError) {
    const key = `error.${e.code}`;
    return has(key) ? t(key) : t('error.unknown', { code: e.code });
  }
  return t('error.unknown', { code: e instanceof Error ? e.message : String(e) });
}
