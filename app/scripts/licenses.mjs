// Builds src/licenses/licenses.json, the list the Open-source licenses
// screen shows (#32): every package the app is built from that is not a
// dev-only package (package-lock.json), with its version, its license and
// the text of its license file; then the Android libraries the build adds
// (scripts/android-libs.json, see scripts/android-libs.mjs) and the fonts
// and shapes that are not packages. Equal license texts are stored once.
// Each entry: n name, v version, l license, t the place of its text in
// `texts` (null: none), k kind (npm, android, other), u an address when
// there is no text.
//
// Run it after any change of dependencies: npm run licenses
// (licenses-test fails until you do: it compares the list with package-lock.json.)

import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const repo = join(app, '..');
const out = join(app, 'src', 'licenses', 'licenses.json');

function fail(msg) {
  console.error(`licenses: ${msg}`);
  process.exit(1);
}

const texts = [];
const textIndex = new Map();
/** textId stores a license text once and returns its place in the list. */
function textId(text) {
  const clean = text.replace(/\r\n/g, '\n').replace(/[ \t]+\n/g, '\n').trim();
  if (!clean) return null;
  const key = createHash('sha256').update(clean).digest('hex');
  if (!textIndex.has(key)) {
    textIndex.set(key, texts.length);
    texts.push(clean);
  }
  return textIndex.get(key);
}

const licenseFile = /^(licen[sc]e|copying|notice|unlicense)([.-].*)?$/i;
function readLicense(dir) {
  if (!existsSync(dir)) return '';
  const names = readdirSync(dir).filter((f) => licenseFile.test(f));
  // LICENSE before NOTICE and COPYING; a plain name before LICENSE-MIT.txt and the like.
  names.sort((a, b) => rank(a) - rank(b) || a.length - b.length || a.localeCompare(b));
  return names.length ? readFileSync(join(dir, names[0]), 'utf8') : '';
}
const rank = (f) => (/^licen[sc]e/i.test(f) ? 0 : /^unlicense/i.test(f) ? 1 : /^copying/i.test(f) ? 2 : 3);

function licenseName(pkg) {
  const l = pkg.license ?? pkg.licenses;
  if (typeof l === 'string') return l;
  if (Array.isArray(l)) return l.map((x) => (typeof x === 'string' ? x : x?.type)).filter(Boolean).join(' OR ');
  if (l && typeof l === 'object' && typeof l.type === 'string') return l.type;
  return '';
}

// 1. The packages of package-lock.json that are not dev-only.
const lock = JSON.parse(readFileSync(join(app, 'package-lock.json'), 'utf8'));
const seen = new Map();
for (const [path, meta] of Object.entries(lock.packages)) {
  if (!path || meta.dev || meta.link) continue;
  const dir = join(app, path);
  const pj = join(dir, 'package.json');
  if (!existsSync(pj)) {
    // An optional package for another system (a Linux or macOS build of a tool) is not installed here.
    if (meta.optional || meta.devOptional || meta.peer) continue;
    fail(`${path} is in package-lock.json but not in node_modules: run npm ci first`);
  }
  const pkg = JSON.parse(readFileSync(pj, 'utf8'));
  const key = `${pkg.name}@${pkg.version}`;
  if (seen.has(key)) continue;
  seen.set(key, { n: pkg.name, v: pkg.version, l: licenseName(pkg) || meta.license || '', t: textId(readLicense(dir)), k: 'npm' });
}
const npm = [...seen.values()].sort((a, b) => a.n.localeCompare(b.n) || a.v.localeCompare(b.v));
if (npm.length < 100) fail(`only ${npm.length} packages found`);

// A package of the React Native repository without a license file of its own
// carries React Native's.
const rnText = seen.get([...seen.keys()].find((k) => k.startsWith('react-native@')))?.t ?? null;
for (const p of npm) if (p.t === null && p.n.startsWith('@react-native/')) p.t = rnText;

// 2. The Android libraries of the release build (scripts/android-libs.json,
// made by scripts/android-libs.mjs from Gradle and the libraries' POMs).
// Apache-2.0 has one text for all; an MIT library of Meta or Expo carries
// that project's text; the others show their license's name and address.
const apache = textId(readFileSync(join(repo, 'LICENSE'), 'utf8'));
const expoText = npm.find((p) => p.n === 'expo')?.t ?? null;
const libs = JSON.parse(readFileSync(join(app, 'scripts', 'android-libs.json'), 'utf8')).libraries;
const android = libs
  .map((x) => {
    let t = null;
    if (x.l === 'Apache-2.0') t = apache;
    else if (x.l === 'MIT' && x.n.startsWith('com.facebook.')) t = rnText;
    else if (x.l === 'MIT' && /^(host\.exp\.exponent|expo\.modules\.|io\.github\.expo)/.test(x.n)) t = expoText;
    return { n: x.n, v: x.v, l: x.l, t, k: 'android', ...(t === null && x.u ? { u: x.u } : {}) };
  })
  .sort((a, b) => a.n.localeCompare(b.n));
if (android.length < 50) fail(`only ${android.length} Android libraries in scripts/android-libs.json`);

// 3. What is in the app but is not a package: KaTeX's fonts (inside the
// katex package, under their own license), the outlines the logo is drawn
// from, and the icon shapes. The same notices as THIRD-PARTY-NOTICES.md.
const notices = readFileSync(join(repo, 'THIRD-PARTY-NOTICES.md'), 'utf8').replace(/\r\n/g, '\n');
function noticeText(heading) {
  const m = new RegExp(`### ${heading.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\n\\n\`\`\`text\\n([\\s\\S]*?)\\n\`\`\``).exec(notices);
  if (!m) fail(`THIRD-PARTY-NOTICES.md has no "${heading}" text`);
  return m[1];
}
const ofl = noticeText('SIL Open Font License 1.1 (KaTeX fonts, Noto Sans KR)');
const other = [
  {
    n: 'KaTeX fonts',
    v: '',
    l: 'OFL-1.1',
    t: textId('Copyright (c) 2009-2010 Design Science, Inc. (www.mathjax.org).\nCopyright (c) 2014-2018 Khan Academy (www.khanacademy.org).\n\n' + ofl),
    k: 'other',
  },
  {
    n: 'Noto Sans KR (outlines in the logo)',
    v: '',
    l: 'OFL-1.1',
    t: textId("Copyright 2014-2021 Adobe (http://www.adobe.com/), with Reserved Font Name 'Source'.\n\n" + ofl),
    k: 'other',
  },
  { n: 'Lucide (icon shapes)', v: '', l: 'ISC', t: textId(noticeText('ISC License (Lucide)')), k: 'other' },
  { n: 'Feather (icon shapes)', v: '', l: 'MIT', t: textId(noticeText('MIT License (Feather)')), k: 'other' },
];

const data = { packages: [...npm, ...android, ...other], texts };
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, JSON.stringify(data) + '\n');
const noText = npm.filter((p) => p.t === null).map((p) => p.n);
const noName = npm.filter((p) => !p.l).map((p) => p.n);
console.log(
  `licenses: ${npm.length} packages, ${android.length} Android libraries, ${other.length} others, ${texts.length} texts ` +
    `(${Math.round(JSON.stringify(data).length / 1024)} KB) -> ${out}`,
);
if (noText.length) console.log(`licenses: no license file in ${noText.length}: ${noText.slice(0, 12).join(', ')}${noText.length > 12 ? ', …' : ''}`);
if (noName.length) console.log(`licenses: no license name in ${noName.length}: ${noName.join(', ')}`);
