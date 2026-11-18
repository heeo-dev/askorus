// What is left of each AI's plan (#20): the tightest window, the states,
// and what the top bar's button sums up.

import type { AdapterInfo, Info } from '@/api/types';

import { aiUsage, lowest, percentLeft, stateOf, summarize, tightest, worst } from './usage';

const ai = (agent: string, usage?: AdapterInfo['usage'], over: Partial<AdapterInfo> = {}): AdapterInfo => ({
  agent,
  available: true,
  capabilities: ['ask'],
  ...(usage ? { usage } : {}),
  ...over,
});
const info = (agents: AdapterInfo[]): Info => ({ name: 'askorus-agent', version: '0.5.0', apiVersion: 1, hostname: 'PC', os: 'windows', capabilities: ['ask'], reserved: [], agents }) as Info;

test('a share left shows as a whole percent, at least 1% above zero', () => {
  expect(percentLeft(0.72)).toBe(72);
  expect(percentLeft(0.001)).toBe(1);
  expect(percentLeft(0)).toBe(0);
  expect(percentLeft(-1)).toBe(0);
  expect(percentLeft(1.4)).toBe(100);
});

test("an AI's bar is its tightest window, a used-up one first", () => {
  const g = [
    { name: '5 hours', left: 0.72, window: '5h' },
    { name: 'Weekly', left: 0.62, window: 'week' },
  ];
  expect(tightest(g)?.window).toBe('week');
  expect(tightest([{ name: 'x', left: 0.3, limited: true }, { name: 'y', left: 0.1 }])?.name).toBe('x');
  expect(tightest([])).toBeUndefined();
  expect(stateOf({ name: 'x', left: 0.19 })).toBe('low');
  expect(stateOf({ name: 'x', left: 0.2 })).toBe('ok');
  expect(stateOf({ name: 'x', left: 0.5, limited: true })).toBe('out');
  expect(stateOf({ name: 'x', left: 0 })).toBe('out');
  expect(aiUsage(ai('agy')).state).toBe('unknown');
});

test('the button sums up the AIs turned on, in the PC order, and names the ones turned off', () => {
  expect(summarize(null, [])).toEqual({ kind: 'loading' });
  const i = info([
    ai('claude', [{ name: '5 hours', left: 0.72, window: '5h' }]),
    ai('codex', [{ name: '5 hours', left: 0, window: '5h', limited: true }]),
    ai('opencode', [{ name: 'Monthly', left: 0.16, window: 'month' }]),
    ai('agy'),
    ai('gemini', undefined, { available: false }),
  ]);
  const s = summarize(i, ['opencode']);
  if (s.kind !== 'ready') throw new Error(s.kind);
  expect(s.ais.map((a) => [a.agent, a.state])).toEqual([
    ['claude', 'ok'],
    ['codex', 'out'],
    ['agy', 'unknown'],
  ]);
  expect(s.off).toEqual(['opencode']);
  expect(worst(s.ais)).toBe('out');
  expect(lowest(s.ais)?.agent).toBe('codex');
  expect(lowest(summarize(i, ['codex']).kind === 'ready' ? (summarize(i, ['codex']) as { ais: ReturnType<typeof aiUsage>[] }).ais : [])?.agent).toBe('opencode');
  expect(summarize(i, ['claude', 'codex', 'opencode', 'agy'])).toEqual({ kind: 'off', off: ['claude', 'codex', 'opencode', 'agy'] });
  expect(worst([aiUsage(ai('agy'))])).toBe('unknown');
});
