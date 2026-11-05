import { AgentError, NetworkError } from '../api/client';
import { compareCodes, synthCodes } from '../ask/compare';
import en from './en.json';
import { errorText, has, label, pickLang, setLang, t } from './index';
import ko from './ko.json';

// P2-3: every code the comparison and its combined answer show has words in both languages.
test('every comparison and combined answer error has words', () => {
  const keys = [
    ...compareCodes.map((c) => `compare.error.${c}`),
    // S5b: the combined answer's own words; its other codes take the comparison's.
    ...synthCodes.map((c) => `compare.synth.error.${c}`),
  ];
  expect(keys.filter((k) => !has(k))).toEqual([]);
  expect(synthCodes.filter((c) => !(compareCodes as readonly string[]).includes(c))).toEqual([]);
});

// Korean screen text is 합니다체, with -세요 for what the person does.
test('Korean comparison sentences end in 니다 or 세요', () => {
  const intros = ['ask.introModerator', 'ask.willSynth'];
  const keys = (Object.keys(ko) as Array<keyof typeof ko>).filter((k) => k.startsWith('compare.') || intros.includes(k));
  expect(keys).toEqual(expect.arrayContaining(['ask.introModerator', 'compare.synth.mayErr', 'compare.synth.error.no_judge']));
  expect(keys.length).toBeGreaterThan(30);
  const bad: string[] = [];
  for (const k of keys) {
    for (const sentence of ko[k].split(/(?<=\.)\s+|\n/)) {
      if (sentence.endsWith('.') && !/(니다|세요)\.$/.test(sentence)) bad.push(`${k}: ${sentence}`);
    }
  }
  expect(bad).toEqual([]);
});

// "{name}이" or "{name}가" is right for one name and wrong for another, so
// no particle that changes with the word before it follows a name.
test('no Korean particle that changes with the name follows a name', () => {
  const after = /\{(name|names|judge|from|against|list)\}(이|가|을|를|은|는|와|과|으로|로)/;
  expect((Object.keys(ko) as Array<keyof typeof ko>).filter((k) => after.test(ko[k]))).toEqual([]);
  expect(after.test('{name}가 답했습니다')).toBe(true);
  expect(after.test('{name}의 답')).toBe(false);
});

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

// S5c (user decision): the not-official line names no company, the slogan
// is the same in both languages.
test('the not-official line is about AI companies, not one of them', () => {
  expect(ko['app.notOfficial']).toBe('AI 회사의 공식 제품이 아닙니다.');
  expect(en['app.notOfficial']).toBe('Not an official product of any AI company.');
  for (const table of [ko, en]) {
    expect(table['app.notOfficial']).not.toMatch(/claude|anthropic|openai/i);
    expect(table['app.slogan']).toBe('Ask, assemble.');
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
  // An activity shows only when it has words (S5c): waiting for input.
  expect(label('activity', 'blocked')).toBe('Waiting for input');
  expect(label('activity', 'busy')).toBe('');
  expect(errorText(new AgentError('brand_new_code', 400))).toBe('Something went wrong (brand_new_code).');
});

test('errors are explained in words', () => {
  setLang('ko');
  expect(errorText(new AgentError('pair_denied', 403))).toBe('PC에서 거절했습니다.');
  expect(errorText(new NetworkError())).toContain('Tailscale');
});
