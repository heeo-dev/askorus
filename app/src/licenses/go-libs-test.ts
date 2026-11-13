// The Go modules of the built-in Tailscale (#57) are listed with their
// licenses by scripts/go-libs.mjs (scripts/go-libs.json for the app's
// screen, THIRD-PARTY-GO.md for the releases). The lists must be made again
// whenever a go.mod changes: this compares them with the go.mod files, as
// licenses-test does with package-lock.json.

import type { Licenses } from '@/app/licenses';

declare const __dirname: string;
const { readFileSync }: { readFileSync(p: string, enc: 'utf8'): string } = require('node:fs');
const { join }: { join(...parts: string[]): string } = require('node:path');

const app = join(__dirname, '..', '..');
const repo = join(app, '..');
const again = 'run `npm run go-libs` and then `npm run licenses` in app/ (Go must be on the PATH), and commit what they write';

/** The modules a go.mod requires, with their versions. */
function required(file: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const line of readFileSync(file, 'utf8').split('\n')) {
    const m = /^\s*(?:require\s+)?([a-z0-9][^\s]*\.[^\s]+)\s+(v[^\s]+)/.exec(line);
    if (m) out.set(m[1], m[2]);
  }
  return out;
}

const libs = JSON.parse(readFileSync(join(app, 'scripts', 'go-libs.json'), 'utf8')) as {
  modules: Array<{ n: string; v: string; l: string; t: number | null }>;
  texts: string[];
};

test("the app's list has the bridge's modules at the versions of tsbridge/go.mod", () => {
  const want = required(join(repo, 'tsbridge', 'go.mod'));
  expect(want.get('tailscale.com')).toBeTruthy();
  const stale = libs.modules.filter((m) => want.has(m.n) && want.get(m.n) !== m.v).map((m) => `${m.n}: listed ${m.v}, go.mod ${want.get(m.n)}`);
  expect({ stale, fix: stale.length ? again : '' }).toEqual({ stale: [], fix: '' });
  const names = libs.modules.map((m) => m.n);
  for (const must of ['tailscale.com', 'golang.org/x/mobile', 'Go standard library and runtime']) expect(names).toContain(must);
  expect(libs.modules.length).toBeGreaterThan(20);
});

test('every module has a license name and its text', () => {
  const bare = libs.modules.filter((m) => !m.l || m.t === null || !libs.texts[m.t]).map((m) => m.n);
  expect(bare).toEqual([]);
});

test('the Open-source licenses screen lists them', () => {
  const data = JSON.parse(readFileSync(join(__dirname, 'licenses.json'), 'utf8')) as Licenses;
  const shown = new Set(data.packages.filter((p) => p.k === 'go').map((p) => `${p.n}@${p.v}`));
  const missing = libs.modules.map((m) => `${m.n}@${m.v}`).filter((x) => !shown.has(x));
  expect({ missing, fix: missing.length ? again : '' }).toEqual({ missing: [], fix: '' });
  for (const p of data.packages.filter((x) => x.k === 'go')) expect(p.t === null ? '' : data.texts[p.t]).toBeTruthy();
});

test('the release notice lists the agent with the versions of agent/go.mod', () => {
  const md = readFileSync(join(repo, 'THIRD-PARTY-GO.md'), 'utf8');
  const agentPart = md.slice(md.indexOf('## PC agent'), md.indexOf('## Phone app'));
  const stale: string[] = [];
  for (const [name, version] of required(join(repo, 'agent', 'go.mod'))) {
    const row = new RegExp(`\\| \`${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\` \\| ([^ ]*) \\|`).exec(agentPart);
    // A module the agent's go.mod names but no built package uses is not in the list.
    if (row && row[1] !== version) stale.push(`${name}: listed ${row[1]}, go.mod ${version}`);
  }
  expect({ stale, fix: stale.length ? again : '' }).toEqual({ stale: [], fix: '' });
  expect(agentPart).toContain('| `tailscale.com` |');
  expect(md).toContain('## License texts');
});
