import type { Info } from '@/api/types';

import { loadAskPrefs, saveAskPrefs } from './prefs';
import { askers, lastSessions, parts, request, running, type Turn } from './thread';

const turn = (over: Partial<Turn>): Turn => ({
  id: 't',
  question: 'q',
  req: { mode: 'ask', prompt: 'q' },
  agents: ['claude', 'codex'],
  ...over,
});

const runOf = (status: string, ps: NonNullable<Turn['run']>['parts']) => ({
  runId: 'r',
  mode: 'ask',
  status,
  startedAt: 'x',
  parts: ps,
});

test('askers keeps the PC order and dims what cannot ask', () => {
  const info = {
    agents: [
      { agent: 'claude', available: true, capabilities: ['list', 'ask'], ask: { tiers: { standard: { model: 'sonnet' } } } },
      { agent: 'codex', available: false, capabilities: [] },
      { agent: 'opencode', available: true, capabilities: ['ask'] },
    ],
  } as unknown as Info;
  expect(askers(info)).toEqual([
    { agent: 'claude', canAsk: true, tiers: { standard: { model: 'sonnet' } } },
    { agent: 'codex', canAsk: false, tiers: {} },
    { agent: 'opencode', canAsk: true, tiers: {} },
  ]);
});

test('a follow-up carries each AI on its latest session, a new AI starts fresh', () => {
  const turns = [
    turn({ run: runOf('done', [{ agent: 'claude', status: 'done', session: 'claude:a' }, { agent: 'codex', status: 'failed', session: 'codex:b' }]) }),
    turn({ agents: ['claude'], run: runOf('done', [{ agent: 'claude', status: 'done', session: 'claude:a2' }]) }),
  ];
  expect(lastSessions(turns)).toEqual({ claude: 'claude:a2', codex: 'codex:b' });
  const all = request({ question: 'more', tier: 'deep', to: 'all', on: ['claude', 'codex', 'opencode'], turns, models: { codex: 'gpt-x' }, id: 'k' });
  expect(all).toEqual({
    mode: 'ask',
    prompt: 'more',
    tier: 'deep',
    clientRunId: 'k',
    agents: [
      { agent: 'claude', session: 'claude:a2' },
      { agent: 'codex', model: 'gpt-x', session: 'codex:b' },
      { agent: 'opencode' },
    ],
  });
  const one = request({ question: 'you', tier: 'quick', to: { agent: 'codex' }, on: ['claude'], turns, models: {}, id: 'k2' });
  expect(one.agents).toEqual([{ agent: 'codex', session: 'codex:b' }]);
});

test('every asked AI has a card, running until the PC says otherwise', () => {
  const t = turn({});
  expect(parts(t).map((p) => p.status)).toEqual(['running', 'running']);
  expect(running(t)).toBe(true);
  const half = turn({ run: runOf('running', [{ agent: 'codex', status: 'done', answer: 'a' }]) });
  expect(parts(half).map((p) => `${p.agent}:${p.status}`)).toEqual(['claude:running', 'codex:done']);
  expect(running(turn({ run: runOf('done', []) }))).toBe(false);
  const lost = turn({ error: "Can't reach the PC" });
  expect(running(lost)).toBe(false);
  expect(parts(lost).map((p) => p.status)).toEqual(['failed', 'failed']);
});

test('ask prefs survive a round trip and bad values fall back', async () => {
  const mem = new Map<string, string>();
  const kv = { get: async (k: string) => mem.get(k) ?? null, set: async (k: string, v: string) => void mem.set(k, v), remove: async () => {} };
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'standard', off: [], models: {}, pc: '' });
  await saveAskPrefs({ tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1' }, kv);
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'deep', off: ['codex'], models: { claude: 'opus' }, pc: 'pc-1' });
  mem.set('ask.v1', JSON.stringify({ tier: 'max', off: 'x', models: { a: 3, b: '' }, pc: 7 }));
  expect(await loadAskPrefs(kv)).toEqual({ tier: 'standard', off: [], models: {}, pc: '' });
  mem.set('ask.v1', '{not json');
  expect((await loadAskPrefs(kv)).tier).toBe('standard');
});
