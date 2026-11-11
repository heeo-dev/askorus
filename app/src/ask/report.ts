// Reporting an AI answer (#50). Google Play asks apps whose main feature is
// talking with generative AI for a way to report offensive output from
// inside the app. Askorus has no server to receive a report, so the app
// writes the report and the person's own mail app sends it: the kind of
// problem, which AI wrote the answer, and, only when the person turns it
// on, the question and the answer. Nothing leaves the phone until they
// press Send there.

import { t } from '@/i18n';

/** Where reports go: the project's public contact address (the same as in the privacy policy). */
export const reportAddress = 'heeo.dev@gmail.com';

export const reportCategories = ['hate', 'sexual', 'violence', 'selfHarm', 'other'] as const;
export type ReportCategory = (typeof reportCategories)[number];

/** What is reported: one AI's answer, the moderator's combined answer, or a debate's result. */
export type ReportSubject = {
  /** Who wrote it, as the screen names it: 'Claude sonnet', 'Combined answer by Codex gpt-5'. */
  ai: string;
  /** The text of the answer, as Copy gives it. */
  text: string;
  /** The question it answers. */
  question?: string;
};

export type ReportMail = { to: string; subject: string; body: string };

// A mail link is one long address: most mail apps take a few thousand
// characters, so a long question or answer goes in by its start.
export const reportQuestionMax = 600;
export const reportAnswerMax = 1800;

function cut(text: string, max: number): string {
  const s = text.trim();
  if (s.length <= max) return s;
  // Not in the middle of a pair (an emoji, a rare Hangul or Han character).
  const end = /[\uD800-\uDBFF]/.test(s[max - 1] ?? '') ? max - 1 : max;
  return `${s.slice(0, end)}\n${t('report.mail.cut')}`;
}

/** reportQuoted is what 'Include the question and the answer' adds to the mail, as the sheet shows it first. */
export function reportQuoted(subject: ReportSubject): string {
  const parts: string[] = [];
  if (subject.question?.trim()) parts.push(t('report.mail.question'), cut(subject.question, reportQuestionMax), '');
  parts.push(t('report.mail.answer'), cut(subject.text, reportAnswerMax));
  return parts.join('\n');
}

/** reportMail writes the report: who it goes to, its subject line and its text. */
export function reportMail(subject: ReportSubject, o: { category: ReportCategory; withText: boolean; version: string; os: string }): ReportMail {
  const kind = t(`report.category.${o.category}`);
  const lines = [
    t('report.mail.kind', { category: kind }),
    t('report.mail.ai', { ai: subject.ai }),
    t('report.mail.app', { version: o.version || '?', os: o.os }),
    '',
    t('report.mail.describe'),
    '',
    '',
  ];
  if (o.withText) lines.push(reportQuoted(subject));
  return { to: reportAddress, subject: t('report.mail.subject', { category: kind }), body: lines.join('\n').trimEnd() + '\n' };
}

/** mailLink is the mailto: address that opens a mail app with the mail written (RFC 6068: lines end with CRLF). */
export function mailLink(mail: ReportMail): string {
  const enc = (s: string) => encodeURIComponent(s.replace(/\r?\n/g, '\r\n'));
  return `mailto:${mail.to}?subject=${enc(mail.subject)}&body=${enc(mail.body)}`;
}

/** mailText is the whole report as plain text, for copying when no mail app opens. */
export function mailText(mail: ReportMail): string {
  return `${mail.to}\n${mail.subject}\n\n${mail.body}`;
}
