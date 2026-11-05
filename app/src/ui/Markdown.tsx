// Answers come back as Markdown (P2-2): bold, lists, tables, code, links,
// footnotes, <details> blocks and math. marked splits the text into tokens
// (with small extensions for footnotes and math, which are not core
// Markdown); this draws them with React Native views in the app's colours,
// so it also runs in Expo Go (no native code). Math in a line becomes
// Unicode text (tex.ts); a formula on lines of its own is drawn by KaTeX
// (MathBlock.tsx). Links open only when they are http(s): an answer must
// not send the phone to another app's scheme.
//
// An answer is whatever an AI wrote, web pages included, so nothing in it
// may stop the app: lexing stays linear in its length, nesting deeper than
// maxNest is drawn flat, KaTeX draws only so much of one answer, and if
// drawing still fails the answer is shown as plain text.
//
// Known: on the Galaxy S10 a line that ends within a pixel of the card's
// edge can be measured as two lines and drawn as one, leaving an empty line
// under it (10-30). Simple line breaking, no selection and a content-sized
// text box did not change it, so it is left as it is; it is rare.

import { Lexer, type Links, Marked, type Token, Tokenizer, type TokenizerExtension, type Tokens } from 'marked';
import { Component, type ReactNode, useMemo, useState } from 'react';
import { Linking, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import { nameParts, plainNames } from '@/ask/aiNames';
import * as i18n from '@/i18n';

import { lettersOrDigits, korean, mathAt, type MathToken, prose } from './inlineMath';
import { MathBlock, drawable } from './MathBlock';
import { texToUnicode } from './tex';
import { type AIColour, aiColour, type Theme, useAIColours, useTheme } from './theme';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

// marked leaves entities such as &nbsp; in the text (and writes & as &amp;
// in some texts), so they are turned into characters here, once. Code is
// left alone: there "&lt;" is what the answer means to show. A Map, so
// "&constructor;" finds nothing instead of Object's own properties.
const named = new Map(
  Object.entries({
    amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ', ensp: ' ', emsp: ' ', thinsp: ' ',
    shy: '­', zwj: '‍', zwnj: '‌', copy: '©', reg: '®', trade: '™', mdash: '—', ndash: '–', hellip: '…',
    laquo: '«', raquo: '»', lsquo: '‘', rsquo: '’', ldquo: '“', rdquo: '”', bull: '•', middot: '·', deg: '°', plusmn: '±',
    times: '×', divide: '÷', minus: '−', le: '≤', ge: '≥', ne: '≠', asymp: '≈', equiv: '≡', sim: '∼', infin: '∞',
    larr: '←', rarr: '→', uarr: '↑', darr: '↓', harr: '↔', lArr: '⇐', rArr: '⇒', hArr: '⇔', crarr: '↵', sect: '§',
    para: '¶', cent: '¢', pound: '£', euro: '€', yen: '¥', curren: '¤', frac12: '½', frac14: '¼', frac34: '¾', sup1: '¹',
    sup2: '²', sup3: '³', micro: 'µ', permil: '‰', prime: '′', Prime: '″', dagger: '†', Dagger: '‡', iexcl: '¡',
    iquest: '¿', check: '✓', cross: '✗', star: '☆', starf: '★', hearts: '♥', spades: '♠', clubs: '♣', diams: '♦',
    sum: '∑', prod: '∏', radic: '√', part: '∂', nabla: '∇', isin: '∈', notin: '∉', and: '∧', or: '∨', cap: '∩', cup: '∪',
    int: '∫', there4: '∴', sub: '⊂', sup: '⊃', empty: '∅', forall: '∀', exist: '∃', lowast: '∗', alpha: 'α', beta: 'β',
    gamma: 'γ', delta: 'δ', epsilon: 'ε', theta: 'θ', lambda: 'λ', mu: 'μ', pi: 'π', sigma: 'σ', tau: 'τ', phi: 'φ',
    omega: 'ω', Delta: 'Δ', Sigma: 'Σ', Omega: 'Ω', Pi: 'Π',
  }),
);
function decode(s: string): string {
  return s.replace(/&(?:#(\d{1,7})|#[xX]([0-9a-fA-F]{1,6})|([A-Za-z][A-Za-z0-9]{1,31}));/g, (m, dec, hex, name) => {
    if (name) return named.get(name) ?? m;
    const n = dec ? Number(dec) : parseInt(hex, 16);
    return n > 0 && n <= 0x10ffff && (n < 0xd800 || n > 0xdfff) ? String.fromCodePoint(n) : '�';
  });
}

/** safeLink is the link to open, or null when it is not a web address. */
export function safeLink(href: string | undefined): string | null {
  if (!href) return null;
  return /^https?:\/\//i.test(href.trim()) ? href.trim() : null;
}

// Nesting deeper than this (lists, quotes, <details>) is drawn flat, as
// written: hundreds of levels would overflow the stack while drawing.
const maxNest = 12;

// Past this many characters, or this many "*" and "_", an answer is drawn as
// plain text from a line break on: real answers are far shorter, and it
// bounds what odd ones cost. (marked reads on to the end for every "*" that
// opens no emphasis: "*a *a *a …" of 50,000 characters took it 46 s.)
const maxMarkdown = 50_000;
const maxMarks = 1000; // about 250 bold phrases

// lineEndsParagraph: a blank line, a heading, a rule or underline, a fence,
// a quote, a list item, a table row or HTML: marked ends a paragraph there.
const lineEndsParagraph =
  /(?:[ \t]*(?:\n|$)| {0,3}(?:#{1,6}(?:[ \t]|\n|$)|([-*_=])(?:[ \t]*\1){2,}[ \t]*(?:\n|$)|`{3}|~{3}|>|[-*+][ \t]|\d{1,9}[.)][ \t]|<|\|))/y;

// blockStart finds where one of our blocks (a footnote or a formula) starts
// inside the current paragraph: marked asks each extension with the text
// from the paragraph's second character on, and stops the paragraph at the
// nearest. Only the paragraph's own lines are read, up to the first block
// of either kind, and once for both extensions, so lexing stays linear in
// the length of the answer.
type BlockStart = { src: string; at?: number; kind?: 'footnote' | 'math' };
let lastStart: BlockStart = { src: '' };

function blockStart(src: string): BlockStart {
  if (lastStart.src === src) return lastStart;
  lastStart = { src };
  for (let at = src.indexOf('\n') + 1; at > 0 && at < src.length; at = src.indexOf('\n', at) + 1) {
    lineEndsParagraph.lastIndex = at;
    if (lineEndsParagraph.test(src)) break;
    footnoteLine.lastIndex = at;
    if (footnoteLine.test(src)) {
      lastStart = { src, at, kind: 'footnote' };
      break;
    }
    blockMathLine.lastIndex = at;
    if (blockMathLine.test(src) && blockMathAt(src, at)) {
      lastStart = { src, at, kind: 'math' };
      break;
    }
  }
  return lastStart;
}

// Footnotes: "[^id]" in the text, "[^id]: words" on a line of its own
// (more lines when indented). They are numbered in the order they are
// first referred to, like GitHub, and listed at the end of the answer.
type FootnoteDef = { type: 'footnoteDef'; raw: string; id: string; text: string; tokens: Token[] };
type FootnoteRef = { type: 'footnoteRef'; raw: string; id: string };

const footnoteLine = / {0,3}\[\^[^\]\s]+\]:/y;

const footnoteDef: TokenizerExtension = {
  name: 'footnoteDef',
  level: 'block',
  start(src) {
    const b = blockStart(src);
    return b.kind === 'footnote' ? b.at : undefined;
  },
  tokenizer(src) {
    const m = /^ {0,3}\[\^([^\]\s]+)\]:[ \t]*([^\n]*(?:\n(?: {2,}|\t)[^\n]*)*)(?:\n+|$)/.exec(src);
    if (!m) return undefined;
    const token: FootnoteDef = { type: 'footnoteDef', raw: m[0], id: m[1], text: m[2].replace(/\n[ \t]+/g, '\n'), tokens: [] };
    this.lexer.inline(token.text, token.tokens);
    return token;
  },
};

// No start(): marked's own text stops before "[" already, and scanning for
// "[^" from every place in a long line made lexing quadratic.
const footnoteRef: TokenizerExtension = {
  name: 'footnoteRef',
  level: 'inline',
  tokenizer(src) {
    const m = /^\[\^([^\]\s]+)\](?!:)/.exec(src);
    return m ? ({ type: 'footnoteRef', raw: m[0], id: m[1] } satisfies FootnoteRef) : undefined;
  },
};

// Math: formulas on lines of their own are read here (blockMathAt); math
// inside a line follows the rules in inlineMath.ts (mathAt), tested by the
// cases in inline-math-cases.json.

const mathInline: TokenizerExtension = {
  name: 'mathInline',
  level: 'inline',
  // Where marked's text must stop for math. The search looks a short way
  // ahead only, so a long line without math stays quick; past it the text
  // just stops once and goes on.
  start(src) {
    const near = src.length > 1000 ? src.slice(0, 1000) : src;
    const i = near.search(/\$|\\[([]/);
    return i >= 0 ? i : near === src ? undefined : near.length - 1;
  },
  tokenizer(src, tokens) {
    const prev = tokens.length ? tokens[tokens.length - 1].raw.slice(-1) : '';
    return mathAt(src, 0, prev) ?? undefined;
  },
};

// Emphasis must not pair a "*" or "_" inside math with one outside it:
// "*최적값은 $f(x^*)$ 이다.*". marked finds emphasis on a copy of the line
// with code and links masked (and "\(" already turned into "++", so the
// hook it offers cannot see the math); the math is masked here from the
// line as written, once per line, and marked's own emphasis code runs on it.
let masked: { from: string; length: number; to: string } | null = null;

function maskMath(src: string, marks: string): string {
  let out = '';
  let from = 0;
  for (let i = 0; i < src.length; i++) {
    if (src[i] !== '$' && src[i] !== '\\') continue;
    const m = mathAt(src, i, src[i - 1] ?? '');
    if (!m) {
      if (src[i] === '\\') i++;
      continue;
    }
    const end = i + m.raw.length;
    out += marks.slice(from, i + 1) + 'a'.repeat(m.raw.length - 2) + marks[end - 1];
    from = end;
    i = end - 1;
  }
  return out + marks.slice(from);
}

function emStrong(this: Tokenizer, src: string, maskedSrc: string, prevChar?: string) {
  if (!masked || masked.from !== maskedSrc || src.length > masked.length) {
    masked = { from: maskedSrc, length: src.length, to: maskMath(src, maskedSrc.slice(maskedSrc.length - src.length)) };
  }
  return Tokenizer.prototype.emStrong.call(this, src, masked.to, prevChar);
}

// The inside stops at the next opening mark too ("\\" pairs skipped, as in
// "\\[4pt]"), so a failed try is short.
const blockMath =
  / {0,3}(?:\$\$((?:(?!\$\$|\n[ \t]*\n)[\s\S])+?)\$\$|\\\[((?:\\\\|(?!\\\]|\\\[|\n[ \t]*\n)[\s\S])+?)\\\])[ \t]*(?:\n+|$)/y;

// blockMathAt reads a formula on lines of its own at src[at], or gives null.
// "\[" alone on its line (as Codex writes it) is always a formula; on one
// line with its words it needs what math in a line needs.
function blockMathAt(src: string, at: number): MathToken | null {
  blockMath.lastIndex = at;
  const m = blockMath.exec(src);
  const text = m ? (m[1] ?? m[2]).trim() : '';
  if (!m || !text) return null;
  if (m[1] !== undefined ? !lettersOrDigits(text) || korean(text) || prose(text) : !/^ {0,3}\\\[[ \t]*\n/.test(m[0]) && !/\\[A-Za-z]|[\^_=+<>]/.test(text)) return null;
  return { type: 'math', raw: m[0], text, display: true };
}

const blockMathLine = / {0,3}(?:\$\$|\\\[)/y;

const mathBlock: TokenizerExtension = {
  name: 'mathBlock',
  level: 'block',
  // Only where a formula really starts may a paragraph stop: a "$$" line
  // that makes no formula would otherwise cut the paragraph there, again
  // and again (lexing grew with the square of the length).
  start(src) {
    const b = blockStart(src);
    return b.kind === 'math' ? b.at : undefined;
  },
  tokenizer(src) {
    return blockMathAt(src, 0) ?? undefined;
  },
};

// Babel's named-capture-group transform (in the React Native preset, so in
// Metro and Jest alike) numbers each regex literal on its own, and marked
// joins two of them into blockSkip: its code-span part then pointed at the
// link part's group, so after `code` in a line no **bold** or *italics* were
// found. The same expression built from a string is left alone by Babel.
// Like marked, it uses lookbehind only where the engine has it.
const lookbehind = (() => {
  try {
    return !!new RegExp('(?<=1)(?<!1)');
  } catch {
    return false;
  }
})();
const blockSkip = new RegExp(
  [
    '\\[(?:[^\\[\\]`]|(`+)[^`]+\\1(?!`))*?\\]\\((?:\\\\[\\s\\S]|[^\\\\\\(\\)]|\\((?:\\\\[\\s\\S]|[^\\\\\\(\\)])*\\))*\\)',
    (lookbehind ? '(?<!`)()' : '(^^|[^`])') + '(`+)[^`]+\\3(?!`)',
    '<(?! )[^<>]*?>',
  ].join('|'),
  'g',
);
for (const rules of Object.values(Lexer.rules.inline)) (rules as { blockSkip: RegExp }).blockSkip = blockSkip;

const md = new Marked({ gfm: true });
md.use({
  extensions: [footnoteDef, footnoteRef, mathBlock, mathInline],
  tokenizer: {
    // Only "~~" strikes text through. A single "~" is a range or "about"
    // ("1~10분", "~3초"), which GFM would strike through between two of them.
    del(src) {
      return /^~~(?!~)/.test(src) ? false : undefined;
    },
    emStrong,
    // marked tries a heading underlined with === or --- at every paragraph,
    // reading on to the next blank line. When footnotes, formulas or
    // headings cut paragraphs short, that read went over the rest of the
    // answer each time; only an underline close by can make one.
    lheading(src) {
      const near = src.length > 4000 ? src.slice(0, 4000) : src;
      const blank = near.search(/\n[ \t]*\n/);
      return /\n {0,3}(?:=+|-+)[ \t]*(?:\n|$)/.test(blank < 0 ? near : near.slice(0, blank + 1)) ? false : undefined;
    },
  },
});

// <details> is gathered into one token at lex time, its inside lexed with
// the answer's link definitions, so footnotes and [1] links work in it.
type DetailsToken = { type: 'details'; raw: string; summary: string; open: boolean; tokens: Token[] };

/** lex splits an answer into tokens: footnotes, math and <details> blocks included. */
export function lex(text: string): Token[] {
  return lexWith(text, {}, 0);
}

function lexWith(text: string, links: Links, depth: number): Token[] {
  const lexer = new md.Lexer(md.defaults);
  Object.assign(lexer.tokens.links, links);
  return tidy(lexer.lex(text), lexer.tokens.links, depth);
}

// HTML that only styles text, by itself on its lines, reads like a paragraph.
const inlineTag = /<\/?(?:a|br|b|i|u|em|strong|sup|sub|span|small|mark|kbd|ins|del|s)(?:\s[^<>]*)?\/?>/gi;
const onlyInlineTags = (raw: string) => !/<[a-z/!?]/i.test(raw.replace(inlineTag, ''));

// uncomment drops complete HTML comments in one pass.
function uncomment(s: string): string {
  let out = '';
  let from = 0;
  for (let a = s.indexOf('<!--'); a >= 0; a = s.indexOf('<!--', from)) {
    const b = s.indexOf('-->', a + 4);
    if (b < 0) break;
    out += s.slice(from, a);
    from = b + 3;
  }
  return out + s.slice(from);
}

// tidy gathers <details>, drops HTML comments (removing one can make
// another, so a few times over) and turns styling HTML into paragraphs.
function tidy(tokens: Token[], links: Links, depth: number): Token[] {
  if (depth >= maxNest) return tokens;
  const out: Token[] = [];
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i];
    if (t.type === 'blockquote') (t as Tokens.Blockquote).tokens = tidy((t as Tokens.Blockquote).tokens, links, depth + 1);
    if (t.type === 'list') for (const it of (t as Tokens.List).items) it.tokens = tidy(it.tokens, links, depth + 1);
    if (t.type !== 'html') {
      out.push(t);
      continue;
    }
    if (/^\s*<details[\s>]/i.test(t.raw)) {
      i = details(tokens, i, links, depth, out);
      continue;
    }
    let rest = t.raw;
    for (let k = 0, next = uncomment(rest); k < 8 && next !== rest; k++, next = uncomment(rest)) rest = next;
    if (rest !== t.raw && /^\s*<details[\s>]/i.test(rest)) {
      tokens[i] = { ...t, raw: rest } as Token; // "<!-- c --><details>…" goes on with the tokens after it
      i--;
    } else if (rest !== t.raw) {
      if (rest.trim()) out.push(...lexWith(rest, links, depth + 1));
    } else if (onlyInlineTags(t.raw)) {
      const lexer = new md.Lexer(md.defaults);
      Object.assign(lexer.tokens.links, links);
      // the line breaks next to the tags would be empty lines
      const text = t.raw.trim().replace(/>[ \t]*\n\s*/g, '>').replace(/\s*\n[ \t]*</g, '<');
      out.push({ type: 'paragraph', raw: t.raw, text, tokens: lexer.inlineTokens(text) } satisfies Tokens.Paragraph);
    } else {
      out.push(t);
    }
  }
  return out;
}

// quiet blanks out code and comments, keeping every other character in its
// place, so tags written inside them are not counted. One pass: a mark with
// no closing one is remembered, not looked for again.
function quiet(raw: string): string {
  let out = '';
  let from = 0;
  let noCommentEnd = false;
  const noCodeEnd = new Set<number>(); // backtick runs with no closing run of the same length
  for (let i = 0; i < raw.length; i++) {
    let end = -1;
    if (!noCommentEnd && raw.startsWith('<!--', i)) {
      const e = raw.indexOf('-->', i + 4);
      if (e < 0) noCommentEnd = true;
      else end = e + 3;
    } else if (raw[i] === '`') {
      let k = 1;
      while (raw[i + k] === '`') k++;
      if (!noCodeEnd.has(k)) {
        const run = '`'.repeat(k);
        let e = raw.indexOf(run, i + k);
        while (e >= 0 && raw[e + k] === '`') e = raw.indexOf(run, e + k + 1);
        if (e < 0) noCodeEnd.add(k);
        else end = e + k;
      }
      if (end < 0) i += k - 1;
    }
    if (end < 0) continue;
    out += raw.slice(from, i) + ' '.repeat(end - i);
    from = end;
    i = end - 1;
  }
  return out + raw.slice(from);
}

// details gathers <details><summary>…</summary>…</details>, which marked
// splits into several tokens when the inside has blank lines, and returns
// the index of the last token it used. Inner <details> are counted, so
// their </details> does not close the outer one; tags in code and comments
// are not counted. What follows </details> in the same token goes back
// among the tokens, so a next <details> right after it ("</details>\n
// <details>", the usual list of questions) is gathered the same way.
function details(tokens: Token[], at: number, links: Links, depth: number, out: Token[]): number {
  let source = '';
  let tail = '';
  let open = 0;
  let j = at;
  for (; j < tokens.length; j++) {
    const raw = tokens[j].raw;
    let end = -1;
    let close = -1;
    if (tokens[j].type !== 'code') {
      const tag = /<(\/?)details(?:\s[^<>]*)?>/gi;
      const seen = quiet(raw);
      for (let m = tag.exec(seen); m && end < 0; m = tag.exec(seen)) {
        if ((open += m[1] ? -1 : 1) === 0) [end, close] = [m.index, m.index + m[0].length];
      }
    }
    if (end < 0) {
      source += raw;
      continue;
    }
    source += raw.slice(0, end);
    tail = raw.slice(close);
    break;
  }
  const head = /^\s*<details([^>]*)>\s*(?:<summary[^>]*>([\s\S]*?)<\/summary>)?/i.exec(source);
  const inner = source.slice(head?.[0].length ?? 0);
  out.push({
    type: 'details',
    raw: source,
    summary: decode(head?.[2]?.replace(/<[^>]+>/g, '') ?? '').trim(),
    open: /(?:^|\s)open(?:[\s=]|$)/i.test(head?.[1] ?? ''),
    tokens: inner.trim() ? lexWith(inner, links, depth + 1) : [],
  } satisfies DetailsToken);
  if (!tail.trim()) return Math.min(j, tokens.length - 1);
  if (/^\s*<details[\s>]/i.test(tail)) {
    tokens[j] = { type: 'html', raw: tail, text: tail, block: true, pre: false } satisfies Tokens.HTML;
    return j - 1;
  }
  out.push(...lexWith(tail, links, depth + 1)); // words after </details> in the same token
  return j;
}

// each calls visit on every token down the tree, inside lists, tables,
// quotes and <details>; visit returns false to skip what is inside one.
function each(tokens: Token[] | undefined, visit: (t: Token) => boolean, depth = 0): void {
  if (depth > 4 * maxNest) return;
  for (const t of tokens ?? []) {
    if (!visit(t)) continue;
    if ('tokens' in t && Array.isArray(t.tokens)) each(t.tokens as Token[], visit, depth + 1);
    if (t.type === 'list') for (const it of (t as Tokens.List).items) each(it.tokens, visit, depth + 1);
    if (t.type === 'table') {
      for (const row of [(t as Tokens.Table).header, ...(t as Tokens.Table).rows]) for (const c of row) each(c.tokens, visit, depth + 1);
    }
  }
}

const superscript = '⁰¹²³⁴⁵⁶⁷⁸⁹';
const sup = (n: number) => [...String(n)].map((d) => superscript[Number(d)]).join('');

// footnoteDefs gives each footnote once: the first definition of an id wins, as on GitHub.
function footnoteDefs(tokens: Token[]): FootnoteDef[] {
  const defs = new Map<string, FootnoteDef>();
  each(tokens, (t) => {
    if (t.type !== 'footnoteDef') return true;
    const d = t as unknown as FootnoteDef;
    if (!defs.has(d.id)) defs.set(d.id, d);
    return false;
  });
  return [...defs.values()];
}

/** footnoteNumbers numbers the defined footnotes in the order they are first referred to. */
export function footnoteNumbers(tokens: Token[]): Map<string, number> {
  const defined = new Set(footnoteDefs(tokens).map((d) => d.id));
  const order = new Set<string>();
  const see = (id: string) => defined.has(id) && order.add(id);
  each(tokens, (t) => {
    if (t.type === 'footnoteRef') see((t as unknown as FootnoteRef).id);
    else if (t.type === 'html') for (const m of t.raw.matchAll(/\[\^([^\]\s]+)\](?!:)/g)) see(m[1]);
    return t.type !== 'footnoteDef';
  });
  for (const id of defined) order.add(id); // defined but never referred to
  return new Map([...order].map((id, i) => [id, i + 1]));
}

// KaTeX runs on the JS thread, so one answer gets only so many formulas
// drawn; the rest are shown as written.
const mathBudget = { count: 40, chars: 12_000 };

type Ctx = {
  s: ReturnType<typeof styles>;
  notes: Map<string, number>;
  depth: number;
  math: { count: number; chars: number };
  /** The AIs a combined answer may name in braces (#6), drawn as chips: name to the AI's own colour, if any. */
  names?: ReadonlyMap<string, AIColour | undefined>;
};

/**
 * Markdown draws an answer. size 'synth' is the combined answer's larger
 * reading size (16/26, F-Result-Expanded); 'body' (15/22) everything else.
 * ais are the AIs that answered: the combined answer writes their names in
 * braces ("{{Claude}}"), drawn as chips (in the AI's own colour, #6) and
 * read without the braces.
 */
export function Markdown({ text, size = 'body', ais }: { text: string; size?: 'body' | 'synth'; ais?: readonly string[] }) {
  const th = useTheme();
  const own = useAIColours().on;
  const s = useMemo(() => styles(th, size), [th, size]);
  const known = useMemo(
    () => (ais ? new Map(ais.map((a) => [agentName(a), own ? aiColour(th, a) : undefined] as const)) : undefined),
    [ais, th, own],
  );
  return (
    <AsText text={known ? plainNames(text) : text} style={s.body}>
      <Drawn text={text} s={s} names={known} />
    </AsText>
  );
}

// chips draws a run of text with the AIs it names in braces as chips: the
// name in its avatar's colour, set off by thin spaces.
function chips(text: string, c: Ctx, key: number | string): ReactNode {
  if (!c.names || !text.includes('{{')) return text;
  return (
    <Text key={key}>
      {nameParts(text, c.names).map((p, i) =>
        typeof p === 'string' ? (
          p
        ) : (
          <Text key={i} style={[c.s.aiName, chipColour(c.names?.get(p.ai))]}>
            {`\u2009${p.ai}\u2009`}
          </Text>
        ),
      )}
    </Text>
  );
}

function chipColour(own: AIColour | undefined) {
  return own ? { backgroundColor: own.bg, color: own.fg } : undefined;
}

// split keeps the part of an answer drawn as Markdown: up to maxMarkdown
// characters and maxMarks emphasis marks, cut at a line break.
function split(text: string): [string, string] {
  let end = Math.min(text.length, maxMarkdown);
  for (let i = 0, marks = 0; i < end; i++) {
    if ((text[i] === '*' || text[i] === '_') && ++marks > maxMarks) end = i;
  }
  if (end === text.length) return [text, ''];
  const cut = text.lastIndexOf('\n', end);
  const at = cut > 0 ? cut : end;
  return [text.slice(0, at), text.slice(at).trim()];
}

function Drawn({ text, s, names }: { text: string; s: Ctx['s']; names?: Ctx['names'] }) {
  const [head, rest] = useMemo(() => split(text), [text]);
  const tokens = useMemo(() => lex(head), [head]);
  const notes = useMemo(() => footnoteNumbers(tokens), [tokens]);
  const c: Ctx = { s, notes, depth: 0, math: { ...mathBudget }, names };
  const defs = footnoteDefs(tokens).sort((a, b) => (notes.get(a.id) ?? 0) - (notes.get(b.id) ?? 0));
  return (
    <View style={s.root}>
      {blocks(tokens, c)}
      {rest ? (
        <Text style={s.body} selectable>
          {rest}
        </Text>
      ) : null}
      {defs.length ? (
        <View style={s.notes} accessibilityLabel={i18n.t('markdown.footnotes')}>
          {defs.map((d) => (
            <Text key={d.id} style={s.note} selectable>
              {sup(notes.get(d.id) ?? 0)} {inline(d.tokens, c)}
            </Text>
          ))}
        </View>
      ) : null}
    </View>
  );
}

// AsText shows the answer as plain text if drawing it fails, so one odd
// answer cannot take the screen (and the chat in memory) down with it.
class AsText extends Component<{ text: string; style: object; children: ReactNode }, { failed: boolean; text: string }> {
  state = { failed: false, text: this.props.text };

  static getDerivedStateFromProps(props: { text: string }, state: { text: string }) {
    return props.text === state.text ? null : { failed: false, text: props.text };
  }

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <Text style={this.props.style} selectable>
        {this.props.text}
      </Text>
    );
  }
}

// Inline HTML tags that only style text: the tag goes, the words stay (an
// <a> left without its pair too). Comments go. Other HTML is shown as
// written, since "<T>" may be part of the answer.
const plainTags = /^<\/?(a|b|i|u|em|strong|sup|sub|span|small|mark|kbd|ins|del|s)(\s[^>]*)?\/?>$/i;

function blocks(tokens: Token[], c: Ctx): ReactNode[] {
  return tokens.map((t, i) => block(t, c, i));
}

function Details({ summary, open: opened, s, children }: { summary: string; open: boolean; s: Ctx['s']; children: ReactNode }) {
  const [open, setOpen] = useState(opened); // <details open> starts open
  return (
    <View style={s.details}>
      <Pressable onPress={() => setOpen((o) => !o)} accessibilityRole="button" accessibilityState={{ expanded: open }} hitSlop={6}>
        <Text style={s.summary}>
          {open ? '▾' : '▸'} {summary || i18n.t('markdown.details')}
        </Text>
      </Pressable>
      {open ? <View style={s.detailsBody}>{children}</View> : null}
    </View>
  );
}

// asWritten shows a token as the answer wrote it.
function asWritten(t: Token, c: Ctx, key: number): ReactNode {
  return 'raw' in t && t.raw.trim() ? (
    <Text key={key} style={c.s.body} selectable>
      {t.raw.trim()}
    </Text>
  ) : null;
}

function block(t: Token, c: Ctx, key: number): ReactNode {
  const { s } = c;
  const nested = t.type === 'blockquote' || t.type === 'list' || t.type === 'details';
  if (nested && c.depth >= maxNest) return asWritten(t, c, key);
  const inside: Ctx = nested ? { ...c, depth: c.depth + 1 } : c;
  switch (t.type) {
    case 'space':
    case 'footnoteDef': // listed at the end
    case 'def': // "[1]: https://…" gives [1] in the text its address; not shown itself
      return null;
    case 'math': {
      // only formulas KaTeX will draw take from the answer's budget
      const tex = (t as unknown as MathToken).text;
      if (!drawable(tex)) return <MathBlock key={key} tex={tex} plain />;
      const plain = c.math.count <= 0 || c.math.chars < tex.length;
      c.math.count -= 1;
      c.math.chars -= tex.length;
      return <MathBlock key={key} tex={tex} plain={plain} />;
    }
    case 'details': {
      const d = t as unknown as DetailsToken;
      return (
        <Details key={key} summary={d.summary} open={d.open} s={s}>
          {blocks(d.tokens, inside)}
        </Details>
      );
    }
    case 'paragraph':
      return (
        <Text key={key} style={s.body} selectable>
          {inline((t as Tokens.Paragraph).tokens, c)}
        </Text>
      );
    case 'text': {
      // a block of text inside a tight list item
      const tt = t as Tokens.Text;
      return (
        <Text key={key} style={s.body} selectable>
          {tt.tokens ? inline(tt.tokens, c) : chips(decode(tt.text), c, 0)}
        </Text>
      );
    }
    case 'heading': {
      const h = t as Tokens.Heading;
      return (
        <Text key={key} style={[s.body, s.heading, h.depth <= 2 ? s.h1 : s.h3]} selectable accessibilityRole="header">
          {inline(h.tokens, c)}
        </Text>
      );
    }
    case 'code':
      return (
        <ScrollView key={key} horizontal style={s.codeBlock} contentContainerStyle={s.codePad}>
          <Text style={s.codeText} selectable>
            {(t as Tokens.Code).text}
          </Text>
        </ScrollView>
      );
    case 'blockquote':
      return (
        <View key={key} style={s.quote}>
          {blocks((t as Tokens.Blockquote).tokens, inside)}
        </View>
      );
    case 'list':
      return list(t as Tokens.List, inside, key);
    case 'table':
      return table(t as Tokens.Table, c, key);
    case 'hr':
      return <View key={key} style={s.hr} />;
    default:
      // other HTML and anything new: show what was written
      return asWritten(t, c, key);
  }
}

function list(l: Tokens.List, c: Ctx, key: number): ReactNode {
  const { s } = c;
  const start = typeof l.start === 'number' ? l.start : 1;
  return (
    <View key={key} style={s.list}>
      {l.items.map((item, i) => (
        <View key={i} style={s.item}>
          <Text style={[s.body, s.bullet]}>{item.task ? (item.checked ? '☑' : '☐') : l.ordered ? `${start + i}.` : '•'}</Text>
          <View style={s.itemBody}>{blocks(item.tokens.filter((x) => x.type !== 'checkbox'), c)}</View>
        </View>
      ))}
    </View>
  );
}

// columnWidths gives every cell of a column one width, from its longest
// text (wide characters such as Hangul count double), so the columns line
// up; each cell sizing itself pushed the columns of long rows aside.
export function columnWidths(rows: string[][]): number[] {
  const len = (t: string) => [...t].reduce((n, ch) => n + (ch.charCodeAt(0) > 0x2e80 ? 2 : 1), 0);
  const cols = Math.max(0, ...rows.map((r) => r.length));
  return Array.from({ length: cols }, (_, i) => {
    const longest = Math.max(0, ...rows.map((r) => len(r[i] ?? '')));
    return Math.min(240, Math.max(80, Math.round(longest * 7.2) + 22));
  });
}

function table(tb: Tokens.Table, c: Ctx, key: number): ReactNode {
  const { s } = c;
  const widths = columnWidths([tb.header, ...tb.rows].map((r) => r.map((cell) => cell.text.replace(/<br\s*\/?>/gi, '\n'))));
  const row = (cells: Tokens.TableCell[], head: boolean, r: number) => (
    <View key={r} style={[s.row, head && s.headRow]}>
      {cells.map((cell, i) => (
        <Text key={i} style={[s.cell, head && s.headCell, { width: widths[i] }]} selectable>
          {inline(cell.tokens, c)}
        </Text>
      ))}
    </View>
  );
  return (
    <ScrollView key={key} horizontal style={s.table}>
      <View>
        {row(tb.header, true, -1)}
        {tb.rows.map((r, i) => row(r, false, i))}
      </View>
    </ScrollView>
  );
}

const isOpenA = (t: Token) => (t.type === 'html' ? /^<a(\s[^>]*)?>$/i.exec(t.raw.trim()) : null);
const isCloseA = (t: Token) => t.type === 'html' && /^<\/a\s*>$/i.test(t.raw.trim());

// anchors turns <a href="…">words</a>, which marked leaves as an opening
// tag, the words and a closing tag, into one link. As in a browser, a new
// <a> ends one left open. The next <a> or </a> is looked for once going
// forward, so many lone <a> stay quick.
function anchors(tokens: Token[]): Token[] {
  const out: Token[] = [];
  let next = 0;
  for (let i = 0; i < tokens.length; i++) {
    const open = isOpenA(tokens[i]);
    if (open && next <= i) for (next = i + 1; next < tokens.length && !isOpenA(tokens[next]) && !isCloseA(tokens[next]); next++);
    if (!open || next >= tokens.length || !isCloseA(tokens[next])) {
      out.push(tokens[i]);
      continue;
    }
    const href = /\shref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))/i.exec(open[1] ?? '');
    const words = tokens.slice(i + 1, next);
    out.push({
      type: 'link',
      raw: tokens.slice(i, next + 1).map((u) => u.raw).join(''),
      href: href ? (href[1] ?? href[2] ?? href[3]) : '',
      title: null,
      text: words.map((u) => u.raw).join(''),
      tokens: words,
    } satisfies Tokens.Link);
    i = next;
  }
  return out;
}

// inline draws a line's tokens. Bold in bold in bold, hundreds deep, would
// overflow the stack while React lays it out, so past 2 × maxNest levels the
// rest is shown as written.
function inline(tokens: Token[] | undefined, c: Ctx, depth = 0): ReactNode[] {
  const { s } = c;
  if (depth > 2 * maxNest) return [(tokens ?? []).map((t) => t.raw).join('')];
  const inner = (ts: Token[] | undefined) => inline(ts, c, depth + 1);
  return anchors(tokens ?? []).map((t, i) => {
    switch (t.type) {
      case 'text': {
        const tt = t as Tokens.Text;
        return tt.tokens ? <Text key={i}>{inner(tt.tokens)}</Text> : chips(decode(tt.text), c, i);
      }
      case 'escape':
        return (t as Tokens.Escape).text;
      case 'strong':
        return (
          <Text key={i} style={s.bold}>
            {inner((t as Tokens.Strong).tokens)}
          </Text>
        );
      case 'em':
        return (
          <Text key={i} style={s.italic}>
            {inner((t as Tokens.Em).tokens)}
          </Text>
        );
      case 'del':
        return (
          <Text key={i} style={s.strike}>
            {inner((t as Tokens.Del).tokens)}
          </Text>
        );
      case 'codespan':
        return (
          <Text key={i} style={s.code}>
            {(t as Tokens.Codespan).text}
          </Text>
        );
      case 'br':
        return '\n';
      case 'checkbox': // a loose task list puts it in the line; the bullet shows it
        return '';
      case 'math': {
        // in a line: Unicode text; what it cannot show stays TeX, as code
        const tex = (t as unknown as MathToken).text;
        const text = texToUnicode(tex);
        return text === null ? (
          <Text key={i} style={s.code}>
            {tex}
          </Text>
        ) : (
          text
        );
      }
      case 'link': {
        const l = t as Tokens.Link;
        const href = safeLink(decode(l.href));
        return (
          <Text
            key={i}
            style={href ? s.link : undefined}
            onPress={href ? () => Linking.openURL(href).catch(() => undefined) : undefined}
            accessibilityRole={href ? 'link' : undefined}
          >
            {inner(l.tokens)}
          </Text>
        );
      }
      case 'image': {
        // images are not loaded; their description stands in
        const im = t as Tokens.Image;
        return decode(im.text || im.href);
      }
      case 'footnoteRef': {
        const n = c.notes.get((t as unknown as FootnoteRef).id);
        return n ? (
          <Text key={i} style={s.ref}>
            {sup(n)}
          </Text>
        ) : (
          t.raw
        );
      }
      case 'html':
        if (/^<br(\s[^>]*)?\/?>$/i.test(t.raw.trim())) return '\n';
        return plainTags.test(t.raw.trim()) || /^<!--[\s\S]*-->$/.test(t.raw.trim()) ? '' : t.raw;
      default:
        return 'raw' in t ? t.raw : '';
    }
  });
}

const styles = (th: Theme, size: 'body' | 'synth' = 'body') =>
  StyleSheet.create({
    root: { gap: size === 'synth' ? 10 : 8 },
    body: size === 'synth' ? { color: th.text, fontSize: 16, lineHeight: 26 } : { color: th.text, fontSize: 15, lineHeight: 22 },
    heading: { fontWeight: '700', marginTop: 2 },
    h1: { fontSize: 18, lineHeight: 25 },
    h3: { fontSize: 16 },
    bold: { fontWeight: '700' },
    italic: { fontStyle: 'italic' },
    strike: { textDecorationLine: 'line-through' },
    code: { fontFamily: mono, fontSize: 14, backgroundColor: th.bg, color: th.text },
    aiName: { fontWeight: '600', backgroundColor: th.avatar, color: th.text },
    link: { color: th.accent, textDecorationLine: 'underline' },
    ref: { color: th.accent, fontWeight: '700' },
    codeBlock: { backgroundColor: th.bg, borderRadius: 8, borderColor: th.border, borderWidth: StyleSheet.hairlineWidth },
    codePad: { padding: 10 },
    codeText: { fontFamily: mono, fontSize: 13, lineHeight: 19, color: th.text },
    quote: { borderLeftWidth: 3, borderLeftColor: th.border, paddingLeft: 10, gap: 6 },
    list: { gap: 4 },
    item: { flexDirection: 'row' },
    bullet: { width: 22 }, // one column for bullets and numbers up to 99.
    itemBody: { flex: 1, gap: 4 },
    hr: { height: StyleSheet.hairlineWidth, backgroundColor: th.border, marginVertical: 4 },
    table: { borderColor: th.border, borderWidth: StyleSheet.hairlineWidth, borderRadius: 6 },
    row: { flexDirection: 'row', borderTopColor: th.border, borderTopWidth: StyleSheet.hairlineWidth },
    headRow: { borderTopWidth: 0, backgroundColor: th.bg },
    cell: { color: th.text, fontSize: 14, lineHeight: 20, paddingHorizontal: 10, paddingVertical: 6 },
    headCell: { fontWeight: '700' },
    details: { borderColor: th.border, borderWidth: StyleSheet.hairlineWidth, borderRadius: 8, padding: 10, gap: 8 },
    summary: { color: th.accent, fontSize: 15, fontWeight: '600' },
    detailsBody: { gap: 8 },
    notes: { borderTopColor: th.border, borderTopWidth: StyleSheet.hairlineWidth, paddingTop: 8, gap: 4 },
    note: { color: th.muted, fontSize: 13, lineHeight: 19 },
  });
