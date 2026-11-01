import { AgentError, NetworkError } from '../api/client';
import { cardKinds, checkNames, compareCodes, labels, synthCodes } from '../ask/compare';
import en from './en.json';
import { errorText, has, label, pickLang, setLang, t } from './index';
import ko from './ko.json';

// P2-3: every code the comparison screens show has words in both languages.
test('every card, check, label and comparison error has words', () => {
  const keys = [
    ...cardKinds.flatMap((k) => [`compare.kind.${k}`, `compare.meaning.${k}`]),
    ...checkNames.map((c) => `compare.why.${c}`),
    ...labels.map((l) => `compare.label.${l}`),
    ...compareCodes.map((c) => `compare.error.${c}`),
    // S5b: the combined answer's own words; its other codes take the comparison's.
    ...synthCodes.map((c) => `compare.synth.error.${c}`),
  ];
  expect(keys.filter((k) => !has(k))).toEqual([]);
  expect(synthCodes.filter((c) => !(compareCodes as readonly string[]).includes(c))).toEqual([]);
});

// Korean screen text is 합니다체, with -세요 for what the person does.
test('Korean comparison sentences end in 니다 or 세요', () => {
  const intros = ['ask.introCompare', 'ask.introSynth'];
  const keys = (Object.keys(ko) as Array<keyof typeof ko>).filter((k) => k.startsWith('compare.') || intros.includes(k));
  expect(keys).toEqual(expect.arrayContaining(['ask.introSynth', 'compare.synth.mayErr', 'compare.synth.error.no_judge']));
  expect(keys.length).toBeGreaterThan(60);
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
