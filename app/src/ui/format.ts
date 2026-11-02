import { type Lang, currentLang, label, t } from '@/i18n';

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

export type SessionStatus = {
  /** The dot: waiting for input (apricot), running (green), or a ring for every other state. */
  kind: 'waiting' | 'running' | 'stopped';
  /** What the row and the sheet say: 입력 기다림, or the state in words. */
  text: string;
};

/**
 * sessionStatus is how a session's state shows in 내 PC (S5c, F-PC): a
 * session waiting for input (the agent's activity 'blocked') says so first,
 * whatever its state; a running one is green; the rest get a ring.
 */
export function sessionStatus(s: { state: string; activity?: string }): SessionStatus {
  if (s.activity === 'blocked') return { kind: 'waiting', text: label('activity', 'blocked') };
  return { kind: s.state === 'running' ? 'running' : 'stopped', text: label('state', s.state) };
}
