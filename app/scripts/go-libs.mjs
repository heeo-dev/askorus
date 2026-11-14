// The Go modules built into the two programs (#57): the PC agent
// (agent/, all three systems) and the app's Tailscale bridge (tsbridge/,
// as built for Android). Both carry Tailscale and what Tailscale is built
// from, so both have to carry those licenses.
//
// Writes:
//   scripts/go-libs.json      the bridge's modules with license names and
//                             texts, which `npm run licenses` puts into the
//                             app's Open-source licenses screen
//   ../THIRD-PARTY-GO.md      both lists and every license text, for the
//                             releases (THIRD-PARTY-NOTICES.md points at it)
//
// Run it after any change of agent/go.mod or tsbridge/go.mod, with Go on
// the PATH, then `npm run licenses`:
//
//   node scripts/go-libs.mjs
//
// (go-libs-test fails until you do: it compares the lists with the go.mod files.)

import { execFileSync } from 'node:child_process';

// The tags the agent and the bridge are built with (tsnode.BuildTags): the
// lists name what is really in the programs.
const TAGS = 'ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail';
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const repo = join(app, '..');
const own = 'github.com/heeo-dev/askorus';

function fail(msg) {
  console.error(`go-libs: ${msg}`);
  process.exit(1);
}

function go(dir, env, args) {
  try {
    return execFileSync('go', args, { cwd: dir, env: { ...process.env, ...env }, encoding: 'utf8', maxBuffer: 64 << 20 });
  } catch (e) {
    fail(`go ${args.join(' ')} in ${dir} failed: ${String(e.stderr || e.message).trim().split('\n').slice(-3).join(' / ')}`);
  }
}

/** The modules a package is built from on one system: path -> { v, dir }. */
function modulesOf(dir, pkg, env) {
  const out = go(dir, env, ['list', '-deps', '-tags', TAGS, '-f', '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}', pkg]);
  const mods = new Map();
  for (const line of out.split('\n')) {
    const [path, v, d] = line.trim().split('|');
    if (!path || path === own || path.startsWith(own + '/')) continue;
    mods.set(path, { v: v || '', dir: d || '' });
  }
  return mods;
}

/** One module by name, whether or not a listed package imports it. */
function moduleByName(dir, name) {
  const [path, v, d] = go(dir, {}, ['list', '-m', '-f', '{{.Path}}|{{.Version}}|{{.Dir}}', name]).trim().split('|');
  return [path, { v, dir: d }];
}

const licenseFile = /^(licen[sc]e|copying|unlicense)([.-].*)?$/i;
function readLicense(dir) {
  if (!dir || !existsSync(dir)) return '';
  const names = readdirSync(dir).filter((f) => licenseFile.test(f));
  names.sort((a, b) => a.length - b.length || a.localeCompare(b));
  return names.length ? readFileSync(join(dir, names[0]), 'utf8') : '';
}

/** The license a text is, by the words every copy of it has. */
function licenseName(text) {
  const t = text.replace(/\s+/g, ' ');
  if (/Apache License,? Version 2\.0/i.test(t)) return 'Apache-2.0';
  if (/Mozilla Public License,? (Version|v\.?) ?2\.0/i.test(t)) return 'MPL-2.0';
  if (/Permission is hereby granted, free of charge, to any person obtaining a copy/i.test(t)) return 'MIT';
  if (/Permission to use, copy, modify, and(\/or)? distribute this software for any purpose with or without fee/i.test(t)) return 'ISC';
  if (/Redistribution and use in source and binary forms/i.test(t)) {
    return /Neither the name|may not be used to endorse or promote/i.test(t) ? 'BSD-3-Clause' : 'BSD-2-Clause';
  }
  if (/This is free and unencumbered software released into the public domain/i.test(t)) return 'Unlicense';
  if (/Creative Commons.*CC0|CC0 1\.0 Universal/i.test(t)) return 'CC0-1.0';
  return '';
}

const clean = (text) => text.replace(/\r\n/g, '\n').replace(/[ \t]+\n/g, '\n').trim();

function describe(mods) {
  const list = [];
  for (const [n, { v, dir }] of [...mods.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    const text = clean(readLicense(dir));
    list.push({ n, v, l: text ? licenseName(text) : '', text });
  }
  return list;
}

// The Go standard library and runtime are in every Go program.
const goroot = go(repo, {}, ['env', 'GOROOT']).trim();
const goVersion = go(join(repo, 'agent'), {}, ['env', 'GOVERSION']).trim().replace(/^go/, '');
const std = { n: 'Go standard library and runtime', v: goVersion, l: 'BSD-3-Clause', text: clean(readLicense(goroot)) };
if (!std.text) fail(`no LICENSE in GOROOT (${goroot})`);

// 1. The agent, as built for each system it is released for.
const agentMods = new Map();
for (const env of [{ GOOS: 'windows', GOARCH: 'amd64' }, { GOOS: 'linux', GOARCH: 'amd64' }, { GOOS: 'darwin', GOARCH: 'arm64' }]) {
  for (const [k, v] of modulesOf(join(repo, 'agent'), './cmd/askorus-agent', { ...env, CGO_ENABLED: '0' })) agentMods.set(k, v);
}
const agent = [std, ...describe(agentMods)];

// 2. The bridge, as built for Android, and gomobile's part that binds it to Java.
const bridgeDir = join(repo, 'tsbridge');
const bridgeMods = modulesOf(bridgeDir, '.', { GOOS: 'android', GOARCH: 'arm64', CGO_ENABLED: '0' });
bridgeMods.set(...moduleByName(bridgeDir, 'golang.org/x/mobile'));
const bridge = [std, ...describe(bridgeMods)];

for (const [what, list] of [
  ['agent', agent],
  ['bridge', bridge],
]) {
  if (list.length < 20) fail(`only ${list.length} modules found for the ${what}`);
  if (!list.some((m) => m.n === 'tailscale.com')) fail(`tailscale.com is not among the ${what}'s modules`);
}

// scripts/go-libs.json: the bridge's modules, equal texts stored once.
const texts = [];
const textIndex = new Map();
function textId(text) {
  if (!text) return null;
  const key = createHash('sha256').update(text).digest('hex');
  if (!textIndex.has(key)) {
    textIndex.set(key, texts.length);
    texts.push(text);
  }
  return textIndex.get(key);
}
const modules = bridge.map((m) => ({ n: m.n, v: m.v, l: m.l, t: textId(m.text) }));
writeFileSync(join(app, 'scripts', 'go-libs.json'), JSON.stringify({ modules, texts }, null, 1) + '\n');

// ../THIRD-PARTY-GO.md: both lists, then every text once with the modules it covers.
const all = new Map(); // text -> { l, names }
for (const m of [...agent, ...bridge]) {
  if (!m.text) continue;
  const e = all.get(m.text) ?? { l: m.l, names: new Set() };
  e.names.add(m.n);
  all.set(m.text, e);
}
const table = (list) =>
  ['| Module | Version | License |', '| --- | --- | --- |', ...list.map((m) => `| \`${m.n}\` | ${m.v || ''} | ${m.l || 'see its text'} |`)].join('\n');
const md = [
  '# Third-party notices: Go modules',
  '',
  'Made by `app/scripts/go-libs.mjs` from `agent/go.mod` and `tsbridge/go.mod`; do not edit it by hand.',
  'The PC agent and the phone app both have Tailscale built in (#57). These are the Go modules each',
  'is built from, with the license of each and, below, every license text. The other notices are in',
  '[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).',
  '',
  '## PC agent (askorus-agent)',
  '',
  'As built for Windows, macOS and Linux.',
  '',
  table(agent),
  '',
  "## Phone app (the Tailscale bridge, `tsbridge`)",
  '',
  'As built for Android. The app shows the same list under Settings > About > Open-source licenses.',
  '',
  table(bridge),
  '',
  '## License texts',
  '',
  ...[...all.entries()]
    .sort((a, b) => [...a[1].names][0].localeCompare([...b[1].names][0]))
    .flatMap(([text, e], i) => [`### ${i + 1}. ${e.l || 'License'}: ${[...e.names].sort().join(', ')}`, '', '```text', text.replace(/```/g, "'''"), '```', '']),
].join('\n');
writeFileSync(join(repo, 'THIRD-PARTY-GO.md'), md);

const noText = [...agent, ...bridge].filter((m) => !m.text).map((m) => m.n);
const noName = [...agent, ...bridge].filter((m) => m.text && !m.l).map((m) => m.n);
const count = (list) => {
  const c = {};
  for (const m of list) c[m.l || '?'] = (c[m.l || '?'] ?? 0) + 1;
  return Object.entries(c)
    .map(([k, v]) => `${k} ${v}`)
    .join(', ');
};
console.log(`go-libs: agent ${agent.length} modules (${count(agent)})`);
console.log(`go-libs: bridge ${bridge.length} modules (${count(bridge)}), ${texts.length} texts`);
if (noText.length) console.log(`go-libs: NO LICENSE FILE in ${[...new Set(noText)].join(', ')}`);
if (noName.length) console.log(`go-libs: license not recognised in ${[...new Set(noName)].join(', ')}`);
