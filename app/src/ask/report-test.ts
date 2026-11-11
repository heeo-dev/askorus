// The report of an AI answer as a mail (#50): what goes into it, what stays
// out unless the person turns it on, and the mail link itself.
import { setLang } from '@/i18n';

import { mailLink, mailText, reportAddress, reportAnswerMax, reportCategories, reportMail, reportQuestionMax, reportQuoted } from './report';

const subject = { ai: 'Claude sonnet', text: 'The answer.\nSecond line.', question: 'What is 2 + 2?' };
const on = { version: '0.1.0', os: 'Android' };

afterEach(() => setLang('en'));

test('a report says the kind of problem, who wrote the answer and the app, and leaves room to describe it', () => {
  const mail = reportMail(subject, { ...on, category: 'hate', withText: false });
  expect(mail.to).toBe(reportAddress);
  expect(mail.subject).toBe('[Askorus report] Hate or discrimination');
  expect(mail.body).toBe(
    ['Problem: Hate or discrimination', 'Written by: Claude sonnet', 'App: Askorus 0.1.0 (Android)', '', 'What was wrong (please write here):', ''].join('\n'),
  );
});

test('the question and the answer stay out unless the person turns them on', () => {
  const without = reportMail(subject, { ...on, category: 'other', withText: false });
  expect(without.body).not.toContain('What is 2 + 2?');
  expect(without.body).not.toContain('The answer.');

  const withText = reportMail(subject, { ...on, category: 'other', withText: true });
  expect(withText.body).toContain('--- Question ---\nWhat is 2 + 2?\n\n--- Answer ---\nThe answer.\nSecond line.\n');
  // What the sheet shows before it goes is exactly what goes.
  expect(withText.body).toContain(reportQuoted(subject));
});

test('an answer with no question is quoted alone', () => {
  expect(reportQuoted({ ai: 'Codex', text: 'Only this.' })).toBe('--- Answer ---\nOnly this.');
  expect(reportQuoted({ ai: 'Codex', text: 'Only this.', question: '   ' })).toBe('--- Answer ---\nOnly this.');
});

test('a long question or answer goes in by its start, and says so', () => {
  const long = { ai: 'Codex', text: 'a'.repeat(reportAnswerMax + 500), question: 'q'.repeat(reportQuestionMax + 100) };
  const quoted = reportQuoted(long);
  expect(quoted).toContain(`${'q'.repeat(reportQuestionMax)}\n(the rest is left out)`);
  expect(quoted).toContain(`${'a'.repeat(reportAnswerMax)}\n(the rest is left out)`);
  expect(quoted).not.toContain('a'.repeat(reportAnswerMax + 1));
  // The link has a ceiling whatever the answer's length. Hangul takes nine
  // signs a letter in a link, so a Korean report is the long one.
  const link = mailLink(reportMail(long, { ...on, category: 'violence', withText: true }));
  expect(link.length).toBeLessThan(4000);
  const korean = { ai: 'Codex', text: '가'.repeat(5000), question: '나'.repeat(2000) };
  expect(mailLink(reportMail(korean, { ...on, category: 'violence', withText: true })).length).toBeLessThan(24000);
});

test('the cut never splits an emoji in two', () => {
  const text = 'a'.repeat(reportAnswerMax - 1) + '😀' + 'tail';
  const quoted = reportQuoted({ ai: 'Codex', text });
  expect(quoted).toContain(`${'a'.repeat(reportAnswerMax - 1)}\n(the rest is left out)`);
  expect(() => encodeURIComponent(quoted)).not.toThrow();
});

test('the mail link carries the address, the subject and the text, with every sign that would break it encoded', () => {
  const mail = reportMail({ ai: 'A & B?', text: 'x=1&y=2 #tag 100% "q"\n한글', question: 'why?' }, { ...on, category: 'sexual', withText: true });
  const link = mailLink(mail);
  expect(link.startsWith(`mailto:${reportAddress}?subject=`)).toBe(true);
  const query = link.slice(link.indexOf('?') + 1);
  // Only the two separators are bare.
  expect(query.split('&')).toHaveLength(2);
  expect(query).not.toMatch(/[#" \n]/);
  const params = Object.fromEntries(query.split('&').map((kv) => kv.split('=').map(decodeURIComponent) as [string, string]));
  expect(params.subject).toBe('[Askorus report] Sexual content');
  // Lines end with CRLF in a mail link.
  expect(params.body).toBe(mail.body.replace(/\n/g, '\r\n'));
  expect(params.body).toContain('x=1&y=2 #tag 100% "q"\r\n한글');
});

test('the report as plain text, for when no mail app opens, starts with where to send it', () => {
  const mail = reportMail(subject, { ...on, category: 'selfHarm', withText: false });
  expect(mailText(mail)).toBe(`${reportAddress}\n[Askorus report] Self-harm\n\n${mail.body}`);
});

test('every kind of problem has a name in both languages, and the mail is written in the app language', () => {
  for (const lang of ['en', 'ko'] as const) {
    setLang(lang);
    for (const category of reportCategories) {
      const mail = reportMail(subject, { ...on, category, withText: false });
      expect(mail.subject).not.toContain('report.category.');
      expect(mail.body).not.toContain('report.');
    }
  }
  setLang('ko');
  const mail = reportMail(subject, { version: '', os: 'Android', category: 'violence', withText: true });
  expect(mail.subject).toBe('[Askorus 신고] 폭력·위험한 행동');
  expect(mail.body).toContain('답을 쓴 AI: Claude sonnet');
  // A version the app could not read shows as unknown, not as a blank.
  expect(mail.body).toContain('앱: Askorus ? (Android)');
  expect(mail.body).toContain('--- 질문 ---');
});
