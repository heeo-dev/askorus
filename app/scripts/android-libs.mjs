// Builds scripts/android-libs.json: the Android libraries of the release
// build with the license each one's POM names (#32). Run it after an
// upgrade of Expo or React Native, then `npm run licenses`:
//
//   cd android
//   gradlew :app:dependencies --configuration releaseRuntimeClasspath --console=plain > deps.txt
//   node ../scripts/android-libs.mjs deps.txt <GRADLE_USER_HOME>
//
// The license names come from the POM files in Gradle's cache (the library's
// own, else its parent's). A library whose POM is not in the cache (the
// Expo modules, which come from node_modules) or names no license takes the
// license of its group below, and is listed in the output so it can be checked.

import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const [depsFile, gradleHome] = process.argv.slice(2);
if (!depsFile || !gradleHome) {
  console.error('usage: node android-libs.mjs <gradle dependencies output> <GRADLE_USER_HOME>');
  process.exit(1);
}
const cache = join(gradleHome, 'caches', 'modules-2', 'files-2.1');
const out = join(dirname(fileURLToPath(import.meta.url)), 'android-libs.json');

// Groups whose libraries come without a POM license here.
const byGroup = [
  [/^(host\.exp\.exponent|expo\.modules\.|io\.github\.expo)/, 'MIT'],
  [/^com\.facebook\.(react|fresco|yoga|infer\.annotation)/, 'MIT'],
  [/^(androidx\.|org\.jetbrains|com\.google\.(guava|code\.gson|dagger|errorprone|android\.material|firebase|android\.datatransport|auto\.value))/, 'Apache-2.0'],
];

// group:artifact -> the version Gradle picked.
const picked = new Map();
for (const line of readFileSync(depsFile, 'utf8').split(/\r?\n/)) {
  const m = /[+\\]--- ([\w.-]+):([\w.-]+):([^\s]+)(?: -> ([^\s]+))?/.exec(line);
  if (!m || / \(c\)$/.test(line)) continue;
  const key = `${m[1]}:${m[2]}`;
  const version = m[4] ?? m[3];
  if (m[4] || !picked.has(key)) picked.set(key, version.replace(/[{}]|strictly /g, ''));
}

function pom(group, artifact, version) {
  const dir = join(cache, group, artifact, version);
  if (!existsSync(dir)) return null;
  for (const hash of readdirSync(dir)) {
    const f = join(dir, hash, `${artifact}-${version}.pom`);
    if (existsSync(f)) return readFileSync(f, 'utf8');
  }
  return null;
}

function license(group, artifact, version, depth = 0) {
  const xml = pom(group, artifact, version);
  if (!xml) return null;
  const block = /<licenses>([\s\S]*?)<\/licenses>/.exec(xml);
  if (block) {
    const names = [...block[1].matchAll(/<name>([^<]+)<\/name>/g)].map((m) => m[1].trim());
    const urls = [...block[1].matchAll(/<url>([^<]+)<\/url>/g)].map((m) => m[1].trim());
    if (names.length) return { name: names.join(' / '), url: urls[0] ?? '' };
  }
  const parent = /<parent>([\s\S]*?)<\/parent>/.exec(xml);
  if (parent && depth < 4) {
    const g = /<groupId>([^<]+)<\/groupId>/.exec(parent[1])?.[1];
    const a = /<artifactId>([^<]+)<\/artifactId>/.exec(parent[1])?.[1];
    const v = /<version>([^<]+)<\/version>/.exec(parent[1])?.[1];
    if (g && a && v) return license(g, a, v, depth + 1);
  }
  return null;
}

// One short name for the common spellings.
function tidy(name) {
  if (/apache/i.test(name) && /2/.test(name)) return 'Apache-2.0';
  if (/^(the )?mit( license)?$/i.test(name.trim())) return 'MIT';
  if (/bsd/i.test(name) && /3/.test(name)) return 'BSD-3-Clause';
  if (/bsd/i.test(name) && /2/.test(name)) return 'BSD-2-Clause';
  return name;
}

const libraries = [];
const guessed = [];
const unknown = [];
for (const [key, version] of [...picked].sort((a, b) => a[0].localeCompare(b[0]))) {
  const [group, artifact] = key.split(':');
  const found = license(group, artifact, version);
  let l = found ? tidy(found.name) : '';
  let u = found?.url ?? '';
  if (!l) {
    l = byGroup.find(([re]) => re.test(group))?.[1] ?? '';
    (l ? guessed : unknown).push(key);
    u = '';
  }
  libraries.push({ n: key, v: version, l, ...(u ? { u } : {}) });
}
writeFileSync(out, JSON.stringify({ libraries }, null, 1) + '\n');
console.log(`android-libs: ${libraries.length} libraries -> ${out}`);
const count = {};
for (const x of libraries) count[x.l || '(none)'] = (count[x.l || '(none)'] ?? 0) + 1;
console.log(Object.entries(count).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${v} ${k}`).join(' | '));
if (guessed.length) console.log(`by group (no POM license): ${guessed.length}: ${guessed.join(', ')}`);
if (unknown.length) console.log(`NO LICENSE FOUND: ${unknown.join(', ')}`);
