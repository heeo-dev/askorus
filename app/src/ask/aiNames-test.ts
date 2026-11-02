// #6: the AIs a combined answer names in braces.

import { nameParts, plainNames } from './aiNames';

const names = new Set(['Claude', 'Codex', 'OpenCode']);

test('marked AIs become parts; the subject stays text', () => {
  expect(nameParts('{{Claude}}가 48~49개, {{OpenCode}}가 49개라고 합니다. Claude Code의 스킬은 따로 셉니다.', names)).toEqual([
    { ai: 'Claude' },
    '가 48~49개, ',
    { ai: 'OpenCode' },
    '가 49개라고 합니다. Claude Code의 스킬은 따로 셉니다.',
  ]);
});

test('a marked name that did not answer stays plain, without braces', () => {
  expect(nameParts('({{Gemini}}만) 그리고 {{Codex}}', names)).toEqual(['(Gemini만) 그리고 ', { ai: 'Codex' }]);
  expect(nameParts('no marks', names)).toEqual(['no marks']);
  expect(nameParts('{{ }} {{a\nb}}', names)).toEqual(['  {{a\nb}}']);
});

test('plainNames takes every pair of braces off', () => {
  expect(plainNames('({{Codex}}만) {{Claude}}와 {{Gemini}}')).toBe('(Codex만) Claude와 Gemini');
});
