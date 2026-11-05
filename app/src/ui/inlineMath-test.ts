// The phone finds math in a line by the rules of inlineMath.ts: every case
// in inline-math-cases.json (shared with the agent's judged units until
// C6d, #24, when it moved here from docs/judge/testdata).

import fixture from './inline-math-cases.json';

import { inlineMathSpans, mathAt, texOfSpan } from './inlineMath';

type Case = { text: string; math: { start: number; end: number; display: boolean }[] };
const cases: Case[] = (fixture as { cases: Case[] }).cases;

test('every shared case gives exactly its spans', () => {
  expect(cases.length).toBeGreaterThanOrEqual(40);
  let spans = 0;
  cases.forEach((c, i) => {
    spans += c.math.length;
    expect({ i, text: c.text, math: inlineMathSpans(c.text) }).toEqual({ i, text: c.text, math: c.math });
  });
  expect(spans).toBeGreaterThanOrEqual(25);
});

test('every shared span is math texOfSpan can read, with the same display', () => {
  for (const c of cases) {
    for (const m of c.math) {
      const raw = c.text.slice(m.start, m.end);
      const tex = texOfSpan(raw);
      expect({ raw, display: tex?.display }).toEqual({ raw, display: m.display });
      expect(tex?.tex).toBe(mathAt(c.text, m.start, c.text[m.start - 1] ?? '')?.text);
    }
  }
});

test('texOfSpan takes off the marks', () => {
  expect(texOfSpan('$\\pi r^2$')).toEqual({ tex: '\\pi r^2', display: false });
  expect(texOfSpan('$ x $')).toEqual({ tex: 'x', display: false });
  expect(texOfSpan('$$x^2$$')).toEqual({ tex: 'x^2', display: true });
  expect(texOfSpan('\\(\\alpha + \\beta\\)')).toEqual({ tex: '\\alpha + \\beta', display: false });
  expect(texOfSpan('\\[\\frac{a}{b}\\]')).toEqual({ tex: '\\frac{a}{b}', display: true });
});

test('texOfSpan gives null for what is not one span of math', () => {
  for (const bad of ['', '$', '$$', '$  $', '$$ $$', '\\(\\)', '\\[ \\]', 'abc', '$x', 'x$', '\\(x\\]', '\\[x\\)', '$$x$', '$x$$']) {
    expect([bad, texOfSpan(bad)]).toEqual([bad, null]);
  }
});

test('inlineMathSpans skips the character after a backslash that starts no math', () => {
  // An escaped dollar opens nothing, and the scan does not try the "$" after the backslash.
  expect(inlineMathSpans('a \\$x$ b')).toEqual([]);
  expect(inlineMathSpans('cost \\$5 and $x$')).toEqual([{ start: 13, end: 16, display: false }]);
  expect(inlineMathSpans('no math here')).toEqual([]);
});
