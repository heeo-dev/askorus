import { setLang } from '@/i18n';

import { ago, folderName, sessionStatus, sessionTitle } from './format';

const now = new Date('2026-10-27T12:00:00Z');
const before = (ms: number) => new Date(now.getTime() - ms).toISOString();

test('ago counts minutes, hours and days, then shows the date', () => {
  expect(ago(before(20_000), now, 'ko')).toBe('방금');
  expect(ago(before(5 * 60_000), now, 'ko')).toBe('5분 전');
  expect(ago(before(3 * 3600_000), now, 'en')).toBe('3 h ago');
  expect(ago(before(2 * 86400_000), now, 'ko')).toBe('2일 전');
  const old = ago('2026-09-01T12:00:00Z', now, 'en');
  expect(old).toBe('2026-09-01');
  expect(ago('not a date', now, 'en')).toBe('');
});

test('folderName shows the last part of Windows and POSIX paths', () => {
  expect(folderName('D:\\ClaudeHome\\Asksemble')).toBe('Asksemble');
  expect(folderName('/home/me/project/')).toBe('project');
  expect(folderName('')).toBe('');
  expect(folderName(undefined)).toBe('');
});

test('sessionTitle falls back to name, last prompt, then a placeholder', () => {
  setLang('en');
  expect(sessionTitle({ title: 'T', name: 'N' })).toBe('T');
  expect(sessionTitle({ title: '', name: 'N' })).toBe('N');
  expect(sessionTitle({ title: '', lastPrompt: 'hello' })).toBe('hello');
  expect(sessionTitle({ title: '' })).toBe('(untitled)');
});

test('sessionStatus puts waiting for input first, then running, then a ring (S5c)', () => {
  setLang('en');
  expect(sessionStatus({ state: 'running', activity: 'blocked' })).toEqual({ kind: 'waiting', text: 'Waiting for input' });
  expect(sessionStatus({ state: 'running', activity: 'busy' })).toEqual({ kind: 'running', text: 'Running' });
  expect(sessionStatus({ state: 'stopped' })).toEqual({ kind: 'stopped', text: 'Stopped' });
  expect(sessionStatus({ state: 'terminal' })).toEqual({ kind: 'stopped', text: 'Terminal' });
  expect(sessionStatus({ state: 'brand-new' })).toEqual({ kind: 'stopped', text: 'brand-new' }); // an unknown state shows its code
  setLang('ko');
  expect(sessionStatus({ state: 'running', activity: 'blocked' }).text).toBe('입력 기다림');
  setLang('en');
});
