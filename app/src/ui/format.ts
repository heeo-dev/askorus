import { type Lang, currentLang, t } from '@/i18n';

/**
 * ago turns a time into "3분 전" / "3 min ago". Hermes has no
 * Intl.RelativeTimeFormat everywhere, so the steps are written out.
 */
export function ago(iso: string, now: Date = new Date(), lang: Lang = currentLang()): string {
  const then = new Date(iso);
  const ms = now.getTime() - then.getTime();
  if (Number.isNaN(ms)) return '';
  const min = Math.floor(ms / 60000);
  if (min < 1) return t('time.now', {}, lang);
  if (min < 60) return t('time.minutes', { n: min }, lang);
  const h = Math.floor(min / 60);
  if (h < 24) return t('time.hours', { n: h }, lang);
  const d = Math.floor(h / 24);
  if (d < 7) return t('time.days', { n: d }, lang);
  const y = then.getFullYear();
  const m = then.getMonth() + 1;
  const day = then.getDate();
  return lang === 'ko' ? `${y}. ${m}. ${day}.` : `${y}-${String(m).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
}

/** folderName shows the last part of a PC path (Windows or POSIX). */
export function folderName(path: string | undefined): string {
  if (!path) return '';
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts[parts.length - 1] ?? path;
}

/** sessionTitle picks the best words to name a session in a list. */
export function sessionTitle(s: { title?: string; name?: string; lastPrompt?: string }): string {
  return s.title || s.name || s.lastPrompt || t('session.untitled');
}
