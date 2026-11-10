// Builds src/ui/katex-style.ts from the installed katex package: KaTeX's CSS
// without its @font-face rules, and every woff2 face as base64 with the CSS
// classes that select it. MathBlock puts them into its WebView page, so
// display math needs no network and no files on the phone.
// Run it after upgrading katex: npm run katex-fonts (MathBlock-test fails
// until you do, since it compares the versions).

import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const katex = join(app, 'node_modules', 'katex');
const dist = join(katex, 'dist');
const out = join(app, 'src', 'ui', 'katex-style.ts');

// Faces any formula may use: Main and Math for letters and symbols, Size1-4
// for big delimiters and operators, AMS for many relations and arrows. The
// rest only come with their classes (\mathcal, \mathfrak, \mathsf, ...).
const always = new Set(['KaTeX_Main', 'KaTeX_Math', 'KaTeX_Size1', 'KaTeX_Size2', 'KaTeX_Size3', 'KaTeX_Size4', 'KaTeX_AMS']);

function fail(msg) {
  console.error(`katex-fonts: ${msg}`);
  process.exit(1);
}

// rules splits a stylesheet into its top-level rules; strings are skipped so
// a brace inside content:"…" does not end a rule.
function rules(css) {
  const list = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < css.length; i++) {
    const ch = css[i];
    if (ch === '"' || ch === "'") {
      const end = css.indexOf(ch, i + 1);
      if (end < 0) fail('unclosed string in the CSS');
      i = end;
    } else if (ch === '{') {
      depth++;
    } else if (ch === '}' && --depth === 0) {
      list.push(css.slice(start, i + 1).trim());
      start = i + 1;
    }
  }
  if (depth !== 0) fail('unbalanced braces in the CSS');
  return list.filter(Boolean);
}

const decl = (body, name) => new RegExp(`(?:^|;)\\s*${name}\\s*:\\s*([^;]+)`).exec(body)?.[1].trim();
const unquote = (s) => s.replace(/^["']|["']$/g, '');
const weights = { normal: '400', bold: '700' };

// selectorClasses gives the classes of the rightmost part of a selector that
// has any (".katex .delim-size1>span" -> delim-size1). An empty list means
// the rule applies to every formula.
function selectorClasses(selector) {
  const parts = selector.trim().split(/\s*[\s>+~]\s*/).reverse();
  for (const part of parts) {
    const classes = [...part.matchAll(/\.([\w-]+)/g)].map((m) => m[1]).filter((c) => c !== 'katex');
    if (classes.length) return classes;
  }
  return [];
}

const { version } = JSON.parse(readFileSync(join(katex, 'package.json'), 'utf8'));
const cssName = ['katex.min.css', 'katex.css'].find((f) => existsSync(join(dist, f)));
if (!cssName) fail(`no katex.min.css or katex.css in ${dist}`);
const css = readFileSync(join(dist, cssName), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');

const kept = [];
const faces = [];
const classesOf = new Map(); // family -> Set of classes, or null when every formula uses it
for (const rule of rules(css)) {
  const open = rule.indexOf('{');
  const head = rule.slice(0, open).trim();
  const body = rule.slice(open + 1, -1);
  if (head === '@font-face') {
    const src = decl(body, 'src') ?? '';
    const woff2 = /url\(\s*["']?([^"')]+\.woff2)["']?\s*\)/.exec(src)?.[1];
    if (!woff2) fail(`no woff2 in ${rule}`);
    const style = decl(body, 'font-style') ?? 'normal';
    const weight = decl(body, 'font-weight') ?? '400';
    faces.push({ family: unquote(decl(body, 'font-family') ?? ''), weight: weights[weight] ?? weight, style, file: woff2 });
    continue;
  }
  kept.push(rule);
  if (head.startsWith('@')) continue; // no @media in KaTeX today; such rules are kept but not searched
  const font = decl(body, 'font-family') ?? decl(body, 'font');
  const family = font && /["']?(KaTeX_[\w-]+)/.exec(font)?.[1];
  if (!family) continue;
  for (const selector of head.split(',')) {
    const classes = selectorClasses(selector);
    if (!classes.length) classesOf.set(family, null);
    else if (classesOf.get(family) !== null) classesOf.set(family, new Set([...(classesOf.get(family) ?? []), ...classes]));
  }
}

if (!faces.length) fail('no @font-face rules found');
const used = new Set(faces.map((f) => f.file.split('/').pop()));
const unused = readdirSync(join(dist, 'fonts')).filter((f) => f.endsWith('.woff2') && !used.has(f));
if (unused.length) console.warn(`katex-fonts: fonts not named by the CSS, left out: ${unused.join(', ')}`);

const quote = (s) => `'${s.replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\n/g, '\\n')}'`;
let bytes = 0;
const fontEntries = faces.map((f) => {
  const data = readFileSync(join(dist, f.file));
  bytes += data.length;
  const set = classesOf.get(f.family);
  if (!always.has(f.family) && !set?.size) console.warn(`katex-fonts: no classes select ${f.family}; it is always included`);
  const classes = always.has(f.family) || !set ? [] : [...set].sort();
  return [
    '  {',
    `    family: ${quote(f.family)},`,
    `    weight: ${quote(f.weight)},`,
    `    style: ${quote(f.style)},`,
    `    classes: [${classes.map(quote).join(', ')}],`,
    '    data:',
    `      ${quote(data.toString('base64'))},`,
    '  },',
  ].join('\n');
});

const ts = `// GENERATED by scripts/katex-fonts.mjs from katex ${version}. Do not edit by hand:
// after upgrading katex run \`npm run katex-fonts\` (MathBlock-test checks the version).
// katexRules is KaTeX's CSS without its @font-face rules. katexFonts holds each
// woff2 face as base64 with the classes that select it; an empty list means
// any formula may need it.
//
// The CSS and the fonts are KaTeX's, not Askorus's (https://katex.org):
//   KaTeX: MIT License, Copyright (c) 2013-2020 Khan Academy and other contributors.
//   KaTeX fonts: SIL Open Font License 1.1,
//     Copyright (c) 2009-2010 Design Science, Inc. (www.mathjax.org),
//     Copyright (c) 2014-2018 Khan Academy (www.khanacademy.org).
// The license texts are in THIRD-PARTY-NOTICES.md at the root of the repository.

export type KatexFont = { family: string; weight: string; style: string; data: string; classes: string[] };

export const katexVersion: string = ${quote(version)};

export const katexRules: string = [
${kept.map((r) => `  ${quote(r)},`).join('\n')}
].join('\\n');

export const katexFonts: KatexFont[] = [
${fontEntries.join('\n')}
];
`;

writeFileSync(out, ts);
console.log(`katex-fonts: katex ${version}, ${kept.length} rules, ${faces.length} faces (${Math.round(bytes / 1024)} KB woff2) -> ${out}`);
