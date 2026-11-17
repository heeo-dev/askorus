// Display names of the AIs the agent may report. They are product names,
// the same in every language.

import { currentLang, has, t } from '../i18n';
import type { AdapterInfo } from './types';

/** aiGuideUrl is the README's section on installing and signing in to the AI programs, in the app's language. */
export function aiGuideUrl(): string {
  return currentLang() === 'ko'
    ? 'https://github.com/heeo-dev/askorus/blob/main/README.ko.md#' + encodeURIComponent('ai-프로그램-설치')
    : 'https://github.com/heeo-dev/askorus#installing-the-ai-programs';
}

const names: Record<string, string> = { claude: 'Claude', codex: 'Codex', opencode: 'OpenCode' };

/** agentName is the name to show for an agent code; unknown codes show as they are. */
export function agentName(agent: string): string {
  return names[agent] ?? agent;
}

/** loginDay is the day an AI's sign-in ends, in the phone's own date form (#60). */
export function loginDay(iso?: string): string {
  const d = new Date(iso ?? '');
  return Number.isNaN(d.getTime()) ? (iso ?? '') : d.toLocaleDateString();
}

/**
 * noticeText turns one of an AI's notice codes into words; unknown codes
 * show as they are. A notice the PC gives a hint for has words of its own
 * (#62: Claude Code on a Mac whose agent was started over SSH).
 */
export function noticeText(code: string, a: Pick<AdapterInfo, 'loginExpiresAt' | 'signInHint'>): string {
  const hinted = a.signInHint ? `notice.${code}_${a.signInHint}` : '';
  const key = hinted && has(hinted) ? hinted : `notice.${code}`;
  return has(key) ? t(key, { date: loginDay(a.loginExpiresAt) }) : code;
}

/** noticeIsNote: a notice that warns ahead (a sign-in that ends soon), not one that says the AI is broken. */
export function noticeIsNote(code: string): boolean {
  return code.endsWith('_login_expiring');
}

/** signInNotice: a notice about the AI's sign-in (#60). */
export function signInNotice(code: string): boolean {
  return code.endsWith('_signed_out') || code.endsWith('_login_expiring');
}
