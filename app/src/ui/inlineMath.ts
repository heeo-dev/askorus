// Math written inside a line of an answer: "$…$", "\(…\)" and the same
// marks for display ("$$…$$", "\[…\]"). Markdown.tsx draws answers with
// these rules, and plainPreview (present.ts) reads card previews by them;
// the cases are in inline-math-cases.json. (Until C6d the agent's judged
// units kept math whole by the same rules.)

// Math: "$$…$$" or "\[…\]" on lines of their own is a formula (display);
// "$…$", "\(…\)" and the same marks inside a line are math in the text.
// "$" is also money and shell, so a "$" opens math only before a non-space
// and not right after a digit or "$" ("5$", "$$$"), and the first "$" after
// it must close it: after a non-space that is not an opening bracket or a
// currency ("(US$)"), and not before a digit or "$" (pandoc's rule, so
// "$5에서 $10" stays money). Math stops at a backtick (code binds tighter:
// "$20 … `$HOME`"); a number followed by Korean ("$20이고") is money, and
// so is a run of English words ("$10 and Pro is 20$"). "$$" in a line needs
// letters or digits too, so "($$)" and "$$ is the PID" stay. "\(…\)" needs
// a command, an operator or a short name like x, and "\[…\]" in a line a
// command or "^": a backslash before a bracket is also Markdown's way to
// write the bracket ("\[options\]", "\(optional\)", "\[Y/n\]").
export type MathToken = { type: 'math'; raw: string; text: string; display: boolean };

const maxInlineMath = 2000; // characters; also keeps every scan short
// "$x$", "$5$", "$α$", "$넓이$": something to show, not only marks ("$–$")
const mathy = (tex: string) => /[A-Za-z0-9Ͱ-Ͽ가-힣\\^_=+<>*/|]/.test(tex);
export const lettersOrDigits = (tex: string) => /[A-Za-z0-9Ͱ-Ͽ]/.test(tex);
// what is left of TeX once commands and \text{…} are taken out
const bare = (tex: string) => tex.replace(/\\(?:text\w*|math\w*|operatorname|mbox)\s*\{[^{}]*\}/g, ' ').replace(/\\[A-Za-z]+/g, ' ');
export const prose = (tex: string) => /(?:^|[\s(])[A-Za-z]{2,}\s+[A-Za-z]{2,}\s+[A-Za-z]{2,}(?=[\s),.;:]|$)/.test(bare(tex));
export const korean = (tex: string) => /[가-힣]/.test(bare(tex));
const currency = /[\s(](?:US|HK|NT|NZ|AU|CA|SG)$/;

// mathAt reads math that starts at src[at], or gives null; prev is the character before it.
export function mathAt(src: string, at: number, prev: string): MathToken | null {
  if (src[at] === '\\') {
    const open = src[at + 1];
    const close = open === '(' ? ')' : open === '[' ? ']' : '';
    if (!close) return null;
    for (let i = at + 2; i < src.length && i - at <= maxInlineMath; i++) {
      if (src[i] === '`') return null; // code binds tighter
      if (src[i] !== '\\') continue;
      if (src[i + 1] === '\\') {
        i++; // "\\", a line break, as in "\\[4pt]"
        continue;
      }
      if (src[i + 1] === open) return null; // a second opening first: not this one
      if (src[i + 1] !== close) continue;
      const text = src.slice(at + 2, i).trim();
      const ok = open === '(' ? /[\\^_=+<>]/.test(text) || /^[A-Za-z0-9]{1,3}$/.test(text) : /\\[A-Za-z]|\^/.test(text);
      return ok ? { type: 'math', raw: src.slice(at, i + 2), text, display: open === '[' } : null;
    }
    return null;
  }
  if (src[at] !== '$' || /[\d$]/.test(prev)) return null;
  const n = src[at + 1] === '$' ? 2 : 1;
  if (!src[at + n] || /[\s$]/.test(src[at + n])) return null;
  for (let i = at + n; i < src.length && i - at <= maxInlineMath; i++) {
    if (src[i] === '\\') i++;
    else if (src[i] === '`') return null;
    else if (src[i] === '$') {
      if (n === 2 && src[i + 1] !== '$') return null;
      const text = src.slice(at + n, i);
      if (/[\s([{]$/.test(text) || currency.test(text) || /[\d$]/.test(src[i + n] ?? '')) return null;
      // "**$HOME** and **$PATH**": the next bold's marks, not the end of math
      if (/\s[*_]+$/.test(text) || /^[*_]+\s/.test(text) || prose(text)) return null;
      if (n === 2 ? !lettersOrDigits(text) || korean(text) : !mathy(text) || /^[\d,.]+\s*[가-힣]/.test(text)) return null;
      return { type: 'math', raw: src.slice(at, i + n), text: text.trim(), display: n === 2 };
    }
  }
  return null;
}

/** Math in a text, marks included: start and end are UTF-16 offsets (string indexes). */
export type MathSpan = { start: number; end: number; display: boolean };

/**
 * inlineMathSpans finds the math in a line the way the shared fixture
 * describes: at each "$" or backslash mathAt is asked; after math the scan
 * goes on at its end, and a backslash that starts no math also skips the
 * character after it.
 */
export function inlineMathSpans(text: string): MathSpan[] {
  const out: MathSpan[] = [];
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== '$' && text[i] !== '\\') continue;
    const m = mathAt(text, i, text[i - 1] ?? '');
    if (!m) {
      if (text[i] === '\\') i++;
      continue;
    }
    out.push({ start: i, end: i + m.raw.length, display: m.display });
    i += m.raw.length - 1;
  }
  return out;
}

/**
 * texOfSpan takes the marks off one span of math as written: "$$…$$" and
 * "\[…\]" are display, "$…$" and "\(…\)" inline. Other marks, or
 * nothing inside them, give null (the caller shows the text as written).
 */
export function texOfSpan(raw: string): { tex: string; display: boolean } | null {
  const marks: Array<[string, string, boolean]> = [
    ['$$', '$$', true],
    ['\\[', '\\]', true],
    ['\\(', '\\)', false],
    ['$', '$', false],
  ];
  for (const [open, close, display] of marks) {
    if (raw.length < open.length + close.length || !raw.startsWith(open) || !raw.endsWith(close)) continue;
    const inside = raw.slice(open.length, raw.length - close.length);
    // "$$x$" is not "$" around "$x"
    if (open === '$' && (inside.startsWith('$') || inside.endsWith('$'))) return null;
    const tex = inside.trim();
    return tex ? { tex, display } : null;
  }
  return null;
}
