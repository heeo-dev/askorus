import { AgentError, NetworkError } from '../api/client';
import en from './en.json';
import { errorText, label, pickLang, setLang, t } from './index';
import ko from './ko.json';

const placeholders = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort();

test('Korean and English have the same keys and placeholders', () => {
  expect(Object.keys(en).sort()).toEqual(Object.keys(ko).sort());
  for (const k of Object.keys(ko) as Array<keyof typeof ko>) {
    expect([k, placeholders(en[k])]).toEqual([k, placeholders(ko[k])]);
    expect(ko[k].trim()).not.toBe('');
  }
});

test('the app name never names Claude or Anthropic', () => {
  for (const table of [ko, en]) {
    expect(table['app.name']).not.toMatch(/claude|anthropic/i);
  }
});

test('Korean phones get Korean, everything else English', () => {
  expect(pickLang('ko')).toBe('ko');
  expect(pickLang('KO')).toBe('ko');
  expect(pickLang('ja')).toBe('en');
  expect(pickLang(undefined)).toBe('en');
});

test('t fills parameters', () => {
  expect(t('home.online', { version: '0.1.0' }, 'en')).toBe('Connected · agent 0.1.0');
});

test('unknown codes from a newer agent still show', () => {
  setLang('en');
  expect(label('state', 'running')).toBe('Running');
  expect(label('state', 'sleeping')).toBe('sleeping');
  expect(label('badge', 'new-badge')).toBe('new-badge');
  expect(errorText(new AgentError('brand_new_code', 400))).toBe('Something went wrong (brand_new_code).');
});

test('errors are explained in words', () => {
  setLang('ko');
  expect(errorText(new AgentError('pair_denied', 403))).toBe('PC에서 거절했습니다.');
  expect(errorText(new NetworkError())).toContain('Tailscale');
});
