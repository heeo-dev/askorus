// The connection check of one PC: the AIs' sign-in (#60).

import type { AgentClient } from '../api/client';
import type { Info } from '../api/types';
import { setLang } from '../i18n';
import { diagnose } from './diagnose';

const base: Info = {
  name: 'askorus-agent',
  version: '0.3.3',
  apiVersion: 1,
  hostname: 'MY-PC',
  os: 'windows',
  agents: [],
  capabilities: [],
  reserved: [],
  integrations: [],
} as unknown as Info;

const client = (info: Info) => ({ info: async () => info }) as unknown as AgentClient;

beforeEach(() => setLang('en'));

test('a signed-out AI fails the check and says how to sign in (#60)', async () => {
  const info = {
    ...base,
    agents: [
      { agent: 'claude', version: '2.1.325', available: true, capabilities: ['list', 'ask'], notices: ['claude_signed_out'] },
      { agent: 'codex', version: '0.167.1', available: true, capabilities: ['ask'], notices: ['codex_signed_out'] },
      // 11-18: OpenCode signs in per provider; the notice is about its model's.
      { agent: 'opencode', version: '2.2.7', available: true, capabilities: ['ask'], notices: ['opencode_signed_out'] },
    ],
  } as Info;
  const checks = await diagnose(client(info));
  const failed = checks.filter((c) => !c.ok).map((c) => c.text);
  expect(failed).toEqual([
    "Claude Code on the PC isn't signed in, so it can't answer. Run claude auth login in a terminal on the PC.",
    "Codex on the PC isn't signed in, so it can't answer. Run codex login in a terminal on the PC.",
    "OpenCode on the PC isn't signed in for its model, so it can't answer. Run opencode auth login in a terminal on the PC and pick the model's provider (OpenCode Go by default).",
  ]);
});

// R-118 (#62): on a Mac whose agent was started over SSH, Claude Code cannot
// read its sign-in; signing in again would not help, starting the agent
// again from the Mac's screen does. An older app (no hint words) or an AI
// without the hint keeps the usual line.
test('a Mac agent started over SSH says to start it again from the screen (#62)', async () => {
  const info = {
    ...base,
    agents: [
      { agent: 'claude', version: '2.1.325', available: true, capabilities: ['list', 'ask'], notices: ['claude_signed_out'], signInHint: 'mac_keychain' },
      { agent: 'codex', version: '0.167.1', available: true, capabilities: ['ask'], notices: ['codex_signed_out'], signInHint: 'some_newer_hint' },
    ],
  } as Info;
  const checks = await diagnose(client(info));
  const failed = checks.filter((c) => !c.ok);
  expect(failed.map((c) => c.text)).toEqual([
    "Claude Code on the PC can't read its sign-in: the agent was started outside the Mac's own screen (over SSH, for example), where the keychain is locked. On the Mac itself, open Terminal and run askorus-agent shutdown, then askorus-agent start.",
    "Codex on the PC isn't signed in, so it can't answer. Run codex login in a terminal on the PC.",
  ]);
  expect(failed.every((c) => c.ai)).toBe(true);
});

test('a sign-in that ends soon is a note with its day (#60)', async () => {
  const at = '2026-11-27T03:02:52Z';
  const info = {
    ...base,
    agents: [{ agent: 'claude', version: '2.1.325', available: true, capabilities: ['list', 'ask'], notices: ['claude_login_expiring'], loginExpiresAt: at }],
  } as Info;
  const checks = await diagnose(client(info));
  const note = checks.find((c) => c.note);
  expect(checks.every((c) => c.ok)).toBe(true);
  expect(note?.text).toBe(`Claude Code's sign-in on the PC ends on ${new Date(at).toLocaleDateString()}. Before then, run claude auth login in a terminal on the PC.`);
});

test('a missing AI says to install its CLI, and the line is marked for the guide (#60)', async () => {
  const info = {
    ...base,
    agents: [
      { agent: 'claude', available: false, capabilities: ['list'], notices: ['claude_not_found'] },
      { agent: 'codex', available: false, capabilities: [], notices: ['codex_not_found'] },
    ],
  } as Info;
  const checks = await diagnose(client(info));
  const ai = checks.filter((c) => c.ai).map((c) => c.text);
  expect(ai).toEqual([
    "Claude Code wasn't found on the PC. To use My PCs, install the Claude Code CLI on the PC and sign in.",
    'Codex: not on this PC (left out of asks). To use it, install the Codex CLI on the PC and sign in.',
  ]);
});

test('an AI that is signed in shows no sign-in line', async () => {
  const info = { ...base, agents: [{ agent: 'claude', version: '2.1.325', available: true, capabilities: ['list', 'ask'] }] } as Info;
  const checks = await diagnose(client(info));
  expect(checks.every((c) => c.ok && !c.note)).toBe(true);
  expect(checks.map((c) => c.text)).toContain('Claude Code 2.1.325');
});
