// R-61 (#4): the agent's cases (agent/internal/judge/testdata/extras.json)
// read the same here, so the phone folds exactly what the combined answer leaves out.

import { extrasStart, splitExtras } from './extras';

// The app has no Node types (tsconfig types: jest), as in brand-test.
declare const __dirname: string;
const { readFileSync }: { readFileSync(p: string, enc: 'utf8'): string } = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');

const cases: { name: string; text: string; main: string }[] = JSON.parse(
  readFileSync(join(__dirname, '..', '..', '..', 'agent', 'internal', 'judge', 'testdata', 'extras.json'), 'utf8'),
);

test('there are cases', () => {
  expect(cases.length).toBeGreaterThanOrEqual(10);
});

test.each(cases.map((c) => [c.name, c] as const))('%s', (_, c) => {
  expect(c.text.slice(0, extrasStart(c.text))).toBe(c.main);
});

test('splitExtras gives the folded part without its leading blank lines', () => {
  const { main, extras } = splitExtras('본문입니다.\n\n**다음에 쓸 문장**\n```\n더 알려줘\n```\n');
  expect(main).toBe('본문입니다.');
  expect(extras).toBe('**다음에 쓸 문장**\n```\n더 알려줘\n```');
  expect(splitExtras('본문입니다.')).toEqual({ main: '본문입니다.', extras: '' });
});
