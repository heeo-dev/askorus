// The list the Open-source licenses screen shows (#32) is made by
// scripts/licenses.mjs. It must be made again whenever the packages change:
// this compares it with package-lock.json, as MathBlock-test does for KaTeX.

import type { Licenses } from '@/app/licenses';

// Node's own modules, typed here: the app has no Node types.
declare const __dirname: string;
const { existsSync, readFileSync }: { existsSync(p: string): boolean; readFileSync(p: string, enc: 'utf8'): string } = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');

const app = join(__dirname, '..', '..');
const data = JSON.parse(readFileSync(join(__dirname, 'licenses.json'), 'utf8')) as Licenses;
const again = 'run `npm run licenses` in app/ and commit src/licenses/licenses.json';

test('lists every package of package-lock.json that is not dev-only, at its version', () => {
  const lock = JSON.parse(readFileSync(join(app, 'package-lock.json'), 'utf8')) as {
    packages: Record<string, { dev?: boolean; link?: boolean }>;
  };
  const want = new Set<string>();
  for (const [path, meta] of Object.entries(lock.packages)) {
    if (!path || meta.dev || meta.link) continue;
    const pj = join(app, path, 'package.json');
    if (!existsSync(pj)) continue; // an optional package for another system
    const pkg = JSON.parse(readFileSync(pj, 'utf8')) as { name: string; version: string };
    want.add(`${pkg.name}@${pkg.version}`);
  }
  const have = new Set(data.packages.filter((p) => p.k === 'npm').map((p) => `${p.n}@${p.v}`));
  const missing = [...want].filter((x) => !have.has(x));
  const extra = [...have].filter((x) => !want.has(x));
  expect({ missing, extra, fix: missing.length || extra.length ? again : '' }).toEqual({ missing: [], extra: [], fix: '' });
  expect(want.size).toBeGreaterThan(300);
});

test('every entry has a name and a kind, and its text is in the list of texts', () => {
  for (const p of data.packages) {
    expect(p.n).toBeTruthy();
    expect(['npm', 'android', 'other']).toContain(p.k);
    if (p.t !== null) expect(data.texts[p.t]?.length ?? 0).toBeGreaterThan(40);
  }
  // Equal texts are stored once.
  expect(new Set(data.texts).size).toBe(data.texts.length);
});

test('the packages the app is made of carry their license text', () => {
  const text = (name: string) => {
    const p = data.packages.find((x) => x.n === name);
    return p && p.t !== null ? data.texts[p.t] : '';
  };
  for (const name of ['react', 'react-native', 'expo', 'marked', 'katex', 'react-native-webview', '@react-native/virtualized-lists']) {
    expect(`${name}: ${text(name).slice(0, 2000)}`).toMatch(/Permission is hereby granted, free of charge/);
  }
  expect(text('katex')).toContain('Khan Academy');
  expect(text('react-native')).toContain('Meta Platforms');
  // No package is left without a license name.
  expect(data.packages.filter((p) => p.k === 'npm' && !p.l).map((p) => p.n)).toEqual([]);
});

test('the Android libraries of the build and the fonts and icon shapes are listed too', () => {
  const android = data.packages.filter((p) => p.k === 'android');
  expect(android.length).toBeGreaterThan(100);
  const lib = (n: string) => android.find((p) => p.n === n);
  expect(lib('com.squareup.okhttp3:okhttp')?.l).toBe('Apache-2.0');
  expect(data.texts[lib('com.squareup.okhttp3:okhttp')?.t ?? -1]).toContain('Apache License');
  expect(data.texts[lib('com.facebook.react:react-android')?.t ?? -1]).toContain('Meta Platforms');
  // A library under terms that are not a text to copy shows where they are.
  const mlkit = lib('com.google.mlkit:barcode-scanning');
  expect(mlkit?.t).toBeNull();
  expect(mlkit?.u).toMatch(/^https:\/\//);
  // No Android library is left without a license name.
  expect(android.filter((p) => !p.l).map((p) => p.n)).toEqual([]);

  const other = data.packages.filter((p) => p.k === 'other');
  const fonts = other.find((p) => p.n === 'KaTeX fonts');
  expect(fonts?.l).toBe('OFL-1.1');
  expect(data.texts[fonts?.t ?? -1]).toContain('SIL OPEN FONT LICENSE Version 1.1');
  expect(data.texts[fonts?.t ?? -1]).toContain('Design Science');
  expect(other.map((p) => p.n)).toEqual(expect.arrayContaining(['Noto Sans KR (outlines in the logo)', 'Lucide (icon shapes)', 'Feather (icon shapes)']));
});
