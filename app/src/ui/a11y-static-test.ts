// R-40: on Android an accessibility value that is set and then left out
// stays behind (the phone check read 'Answering…' on a tab after the answer
// came, because TurnTabs passed undefined once there was no mark). So no
// screen may pass accessibilityValue that can be undefined or null: it is
// always an object, an empty one ({ text: '' }) when there is nothing to say.
//
// This reads the source of every screen and component, since the bug only
// shows on a phone.

// Node's fs and path, typed here: the app's tsconfig has no Node types.
declare const __dirname: string;
const { readdirSync, readFileSync, statSync }: {
  readdirSync(p: string): string[];
  readFileSync(p: string, encoding: 'utf8'): string;
  statSync(p: string): { isDirectory(): boolean };
} = require('node:fs');
const { join, relative }: { join(...parts: string[]): string; relative(from: string, to: string): string } = require('node:path');

const src = join(__dirname, '..');

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === 'test' ? [] : files(p);
    return /\.tsx$/.test(name) && !/-test\.tsx$/.test(name) ? [p] : [];
  });
}

/** values gives the expression of every accessibilityValue={…} in a source. */
export function values(code: string): string[] {
  const out: string[] = [];
  const re = /accessibilityValue=\{/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(code))) {
    let depth = 1;
    let i = m.index + m[0].length;
    const start = i;
    for (; i < code.length && depth > 0; i++) {
      const c = code[i];
      if (c === '"' || c === "'" || c === '`') {
        const end = code.indexOf(c, i + 1);
        i = end < 0 ? code.length : end;
      } else if (c === '{') depth++;
      else if (c === '}') depth--;
    }
    out.push(code.slice(start, i - 1).trim());
  }
  return out;
}

/** offends says why an accessibilityValue expression may come out undefined, or ''. */
export function offends(expr: string, code: string): string {
  if (/\b(undefined|null)\b/.test(expr)) return 'can be undefined or null';
  if (expr.includes('&&')) return 'can be false (&&)';
  // A plain reference is fine only when nothing in the file declares the value optional.
  if (/^[\w$.]+$/.test(expr) && /accessibilityValue\?\s*:/.test(code)) return 'passes an optional value on';
  return '';
}

test('the scanner catches the R-40 shape and lets objects through', () => {
  const turnTabs = "accessibilityValue={tab.mark ? { text: t(tab.mark === '…' ? 'ask.answering' : 'compare.tab.failed') } : undefined}";
  expect(values(turnTabs)).toEqual(["tab.mark ? { text: t(tab.mark === '…' ? 'ask.answering' : 'compare.tab.failed') } : undefined"]);
  expect(offends(values(turnTabs)[0], turnTabs)).not.toBe('');
  expect(offends('busy && { text: x }', '')).not.toBe('');
  expect(offends('{ text: busy ? a : b }', '')).toBe('');
  expect(offends('accessibilityValue ?? noValue', 'accessibilityValue?: { text: string }')).toBe('');
  expect(offends('accessibilityValue', 'accessibilityValue?: { text: string }')).not.toBe('');
  expect(offends('r.accessibilityValue', 'accessibilityValue: { text: string }')).toBe('');
  expect(values('<A accessibilityValue={{ text: `${n} {x}` }} />')).toEqual(['{ text: `${n} {x}` }']);
});

test('no screen or component passes an accessibilityValue that can be left out (R-40)', () => {
  const bad: string[] = [];
  let seen = 0;
  for (const f of files(src)) {
    const name = relative(src, f).replace(/\\/g, '/');
    const code = readFileSync(f, 'utf8');
    for (const v of values(code)) {
      seen++;
      const why = offends(v, code);
      if (why) bad.push(`${name}: accessibilityValue={${v}} ${why}`);
    }
  }
  expect(bad).toEqual([]);
  expect(seen).toBeGreaterThan(0); // the scan found the components that set one
});
