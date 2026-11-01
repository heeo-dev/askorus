import { AgentClient, CancelledError } from '@/api/client';
import type { AdapterInfo, Comparison, Info, Run } from '@/api/types';

import { compareState, synthState } from './compare';
import {
  askers,
  type FollowDeps,
  followCompare,
  followRun,
  lastSessions,
  newTurn,
  parts,
  request,
  running,
  stopAll,
  type Turn,
} from './thread';

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

// ---- P2-3: a turn's comparison ----

const all = ['claude', 'codex', 'opencode'];
const judge = (agent: string, family: string, caps = ['ask', 'judge']): AdapterInfo => ({
  agent,
  available: true,
  capabilities: caps,
  ...(caps.includes('judge') ? { judge: { model: `${agent}-quick`, family } } : {}),
});
const infoOf = (agents: AdapterInfo[]) =>
  ({ name: 'asksemble-agent', version: '0.3.0', apiVersion: 1, hostname: 'pc', os: 'windows', agents, capabilities: ['ask'], reserved: [] }) as Info;
const info3 = infoOf([judge('claude', 'anthropic'), judge('codex', 'openai'), judge('opencode', 'deepseek')]);
const ask = (over: Partial<Parameters<typeof newTurn>[0]> = {}) =>
  newTurn({
    id: 't1',
    question: 'Capital?',
    tier: 'quick',
    to: 'all',
    on: all,
    turns: [],
    models: {},
    info: info3,
    auto: true,
    synth: { on: true, agent: 'claude' },
    at: 5,
    ...over,
  });

const answers = [
  { agent: 'claude', status: 'done', answer: 'Seoul. 9.4 million.', model: 'claude-standard', session: 'claude:s1' },
  { agent: 'codex', status: 'done', answer: 'Seoul. 9.7 million.', model: 'codex-standard', session: 'codex:s1' },
  { agent: 'opencode', status: 'done', answer: 'Seoul. 9.7 million.', model: 'opencode-standard', session: 'opencode:s1' },
];
const runAt = (status: string, ps: unknown[], over: Partial<Run> = {}) => ({ ...runOf(status, ps as Run['parts']), runId: 'r1', pollAfterMs: 1, ...over });
const snap = (id: string, status: string, over: Partial<Comparison> = {}): Comparison => ({
  comparisonId: id,
  status,
  source: 'run',
  engine: 'pc',
  answers: [],
  requests: [],
  judgments: [],
  items: [],
  counts: { differ: 0, check: 0, agree: 0, onlyOne: 0, notCompared: 0, notJudged: 0 },
  ...(status === 'waiting' || status === 'running' ? { pollAfterMs: 1 } : {}),
  ...over,
});
const failedPair = { id: 'r1', from: 'claude', against: 'codex', judge: 'opencode', judgeOnly: false, status: 'failed', attempt: 1, error: { code: 'judge_failed', message: 'usage limit reached' } };

type Call = { method: string; path: string; body?: Record<string, unknown> };
type Reply = [status: number, body: unknown] | 'drop' | ((c: Call) => [number, unknown] | 'drop');

// script plays a PC: each "METHOD path" answers from its own list in order
// (the last answer repeats), and every call is recorded.
function script(routes: Record<string, Reply[]>) {
  const calls: Call[] = [];
  const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const c: Call = {
      method: init?.method ?? 'GET',
      path: String(input).replace('https://pc.test/v1/', ''),
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    };
    calls.push(c);
    const list = routes[`${c.method} ${c.path}`];
    let r: Reply | undefined = list && (list.length > 1 ? list.shift() : list[0]);
    if (typeof r === 'function') r = r(c);
    if (!r) r = [500, { error: { code: 'unexpected', message: `${c.method} ${c.path}` } }];
    if (r === 'drop') throw new TypeError('Network request failed');
    return new Response(JSON.stringify(r[1]), { status: r[0], headers: { 'Content-Type': 'application/json' } });
  }) as typeof fetch;
  // Like the real sleep, a stopped follow ends at its next wait.
  const sleep = async (_ms: number, signal?: AbortSignal) => {
    if (signal?.aborted) throw new CancelledError();
  };
  return { client: new AgentClient('https://pc.test', { fetch: fetchFn, sleep }), calls, routes };
}
const said = (calls: Call[]) => calls.map((c) => `${c.method} ${c.path}`);

// harness keeps the turns like the screen does and records each patch.
function harness(first: Turn) {
  let turns = [first];
  let ids = 0;
  const seen: Turn[] = [];
  const deps: FollowDeps = {
    follows: new Map(),
    update: (id, patch) => {
      turns = turns.map((x) => (x.id === id ? { ...x, ...patch(x) } : x));
      seen.push(turns[0]);
    },
    newId: () => `key-${++ids}`,
    errorText: (e) => `words for ${(e as Error).name}`,
  };
  return { deps, seen, now: () => turns[0] };
}

test('newTurn with auto compare asks the PC to compare, with the judges and earlier questions', () => {
  const t = ask();
  expect(t.req).toEqual({
    mode: 'ask',
    prompt: 'Capital?',
    tier: 'quick',
    clientRunId: 't1',
    agents: [{ agent: 'claude' }, { agent: 'codex' }, { agent: 'opencode' }],
    compare: { earlier: [], judges: all },
  });
  expect(t).toMatchObject({ id: 't1', question: 'Capital?', agents: all, at: 5, compare: { start: 'auto', judges: all, earlier: [] } });

  const first = { ...t, run: runAt('done', answers) };
  const only = { ...ask({ id: 't2', to: { agent: 'codex' } }), question: 'only codex', run: runAt('done', [answers[1]]) };
  const third = ask({ id: 't3', question: 'more', turns: [first, only] });
  expect(third.req.compare).toEqual({ earlier: ['Capital?'], judges: all });
  expect(third.req.agents?.map((a) => a.session)).toEqual(['claude:s1', 'codex:s1', 'opencode:s1']);
});

test('newTurn without auto compare, with two AIs, to one AI, or on an older agent', () => {
  const manual = ask({ auto: false });
  expect('compare' in manual.req).toBe(false);
  expect(manual.compare).toEqual({ start: 'manual', judges: all, earlier: [] });

  const two = ask({ on: ['claude', 'codex'] });
  expect('compare' in two.req).toBe(false);
  expect(two.compare).toEqual({ start: 'noJudgeOff', judges: [], earlier: [] });
  const twoOnly = ask({ on: ['claude', 'codex'], info: infoOf([judge('claude', 'anthropic'), judge('codex', 'openai')]) });
  expect(twoOnly.compare).toEqual({ start: 'noJudgeNone', judges: [], earlier: [] });

  const one = ask({ to: { agent: 'codex' } });
  expect(one.agents).toEqual(['codex']);
  expect(one.compare).toBeUndefined();
  expect('compare' in one.req).toBe(false);

  const old = ask({ info: infoOf([judge('claude', 'anthropic', ['ask']), judge('codex', 'openai', ['ask']), judge('opencode', 'deepseek', ['ask'])]) });
  expect(old.compare).toBeUndefined();
  expect('compare' in old.req).toBe(false);
});

test('a run with a comparison is followed to the end of the comparison', async () => {
  const pc = script({
    'POST runs': [[202, runAt('running', [], { comparisonId: 'c1' })]],
    'GET runs/r1': [
      [200, runAt('running', [answers[0]], { comparisonId: 'c1' })],
      [200, runAt('done', answers, { comparisonId: 'c1', pollAfterMs: undefined })],
    ],
    'GET comparisons/c1': [[200, snap('c1', 'waiting')], [200, snap('c1', 'running')], [200, snap('c1', 'done')]],
  });
  const h = harness(ask());
  await followRun(pc.client, h.now(), h.deps);
  expect(said(pc.calls)).toEqual(['POST runs', 'GET runs/r1', 'GET runs/r1', 'GET comparisons/c1', 'GET comparisons/c1', 'GET comparisons/c1']);
  expect(pc.calls[0].body?.compare).toEqual({ earlier: [], judges: all });
  expect(h.now().run?.status).toBe('done');
  expect(h.now().compare?.id).toBe('c1');
  expect(h.seen.map((t) => t.compare?.snap?.status).filter(Boolean)).toEqual(['waiting', 'running', 'done']);
  expect(compareState(h.now())).toEqual({ kind: 'done', partial: false, retry: false });
  expect(h.deps.follows.size).toBe(0);
});

test('a manual turn compares only when asked, and the run error stays words', async () => {
  const pc = script({
    'POST runs': [[202, runAt('done', answers, { pollAfterMs: undefined })]],
  });
  const h = harness(ask({ auto: false }));
  await followRun(pc.client, h.now(), h.deps);
  expect(said(pc.calls)).toEqual(['POST runs']);
  expect(compareState(h.now())).toEqual({ kind: 'manual', cost: 6 });

  const down = script({ 'POST runs': ['drop'] });
  const g = harness(ask());
  await followRun(down.client, g.now(), g.deps);
  expect(g.now().error).toBe('words for NetworkError');
  expect(said(down.calls)).toEqual(['POST runs', 'POST runs', 'POST runs']);
});

test('a lost comparison request is sent again with the same key, an answered one is not', async () => {
  const pc = script({ 'POST comparisons': ['drop'] });
  const h = harness({ ...ask({ auto: false }), run: runAt('done', answers) });
  await followCompare(pc.client, h.now(), 'start', h.deps);
  expect(h.now().compare).toMatchObject({ key: 'key-1', error: 'network' });
  expect(compareState(h.now())).toEqual({ kind: 'lost', action: 'start' });

  pc.routes['POST comparisons'] = ['drop', [202, snap('c7', 'waiting', { source: 'direct' })]];
  pc.routes['GET comparisons/c7'] = [[200, snap('c7', 'done', { source: 'direct' })]];
  await followCompare(pc.client, h.now(), 'start', h.deps);
  expect(said(pc.calls)).toEqual([...Array(5).fill('POST comparisons'), 'GET comparisons/c7']);
  expect(pc.calls.slice(0, 5).map((c) => c.body?.clientComparisonId)).toEqual(Array(5).fill('key-1'));
  expect(pc.calls[0].body).toMatchObject({ question: 'Capital?', earlier: [], judges: all });
  expect((pc.calls[0].body?.answers as unknown[]).length).toBe(3);
  expect(h.now().compare).toMatchObject({ id: 'c7', key: undefined, error: undefined });
  expect(compareState(h.now())).toEqual({ kind: 'done', partial: false, retry: false });

  // An answer from the PC (here 429) ends that key: the next start makes a new one.
  const busy = script({ 'POST comparisons': [[429, { error: { code: 'too_many_comparisons', message: 'busy' } }]] });
  const g = harness({ ...ask({ auto: false }), run: runAt('done', answers) });
  await followCompare(busy.client, g.now(), 'start', g.deps);
  expect(g.now().compare).toMatchObject({ key: undefined, error: 'too_many_comparisons' });
  expect(compareState(g.now())).toEqual({ kind: 'failed', code: 'too_many_comparisons', action: 'start' });
  await followCompare(busy.client, g.now(), 'start', g.deps);
  expect(busy.calls.map((c) => c.body?.clientComparisonId)).toEqual(['key-1', 'key-2']);
});

test('a comparison the PC lost shows as not found, and 다시 비교 makes a new one', async () => {
  const pc = script({
    'GET comparisons/c1': [[404, { error: { code: 'comparison_not_found', message: 'gone' } }]],
    'POST comparisons': [[202, snap('c2', 'waiting', { source: 'direct' })]],
    'GET comparisons/c2': [[200, snap('c2', 'running')], [200, snap('c2', 'done')]],
  });
  const t = ask();
  const h = harness({ ...t, run: runAt('done', answers), compare: { ...t.compare!, id: 'c1', snap: snap('c1', 'running') } });
  await followCompare(pc.client, h.now(), 'poll', h.deps);
  expect(h.now().compare?.error).toBe('comparison_not_found');
  expect(compareState(h.now())).toEqual({ kind: 'notFound' });
  // stopAll leaves a comparison the PC no longer has alone.
  stopAll(pc.client, [h.now()], h.deps.follows);
  expect(said(pc.calls)).toEqual(['GET comparisons/c1']);

  await followCompare(pc.client, h.now(), 'start', h.deps);
  // While the new request was on its way, the old snapshot was gone.
  expect(h.seen.some((x) => x.compare?.key === 'key-1' && !x.compare.snap && compareState(x)?.kind === 'comparing')).toBe(true);
  expect(said(pc.calls)).toEqual(['GET comparisons/c1', 'POST comparisons', 'GET comparisons/c2', 'GET comparisons/c2']);
  expect(h.now().compare).toMatchObject({ id: 'c2', key: undefined, error: undefined });
  expect(compareState(h.now())?.kind).toBe('done');
});

test('다시 비교 asks the PC to judge again; a comparison still running is just followed', async () => {
  const failed = snap('c1', 'done', { requests: [failedPair] });
  const t = ask();
  const pc = script({
    'POST comparisons/c1/retry': [[409, { error: { code: 'comparison_running', message: 'running' } }], [202, snap('c1', 'running')]],
    'GET comparisons/c1': [[200, snap('c1', 'running')], [200, snap('c1', 'done')]],
  });
  const h = harness({ ...t, run: runAt('done', answers), compare: { ...t.compare!, id: 'c1', snap: failed } });
  expect(compareState(h.now())).toEqual({ kind: 'done', partial: true, retry: true });
  await followCompare(pc.client, h.now(), 'retry', h.deps);
  expect(said(pc.calls)).toEqual(['POST comparisons/c1/retry', 'GET comparisons/c1', 'GET comparisons/c1']);
  expect(h.now().compare?.error).toBeUndefined();
  expect(compareState(h.now())).toEqual({ kind: 'done', partial: false, retry: false });

  pc.routes['GET comparisons/c1'] = [[200, snap('c1', 'done')]];
  await followCompare(pc.client, h.now(), 'retry', h.deps);
  expect(said(pc.calls).slice(3)).toEqual(['POST comparisons/c1/retry', 'GET comparisons/c1']);
  expect(h.seen.some((x) => x.compare?.snap?.status === 'running')).toBe(true);
});

test('a follow that was replaced patches nothing more', async () => {
  const t = ask();
  const h = harness({ ...t, run: runAt('done', answers), compare: { ...t.compare!, id: 'c1' } });
  const pc = script({
    'GET comparisons/c1': [
      () => {
        // A newer follow (or 새 질문) takes over while this answer is on its way.
        h.deps.follows.get('t1/compare')?.abort();
        return [200, snap('c1', 'running')];
      },
    ],
  });
  await followCompare(pc.client, h.now(), 'poll', h.deps);
  expect(h.now().compare?.snap).toBeUndefined();
  expect(h.now().compare?.error).toBeUndefined();
  expect(said(pc.calls)).toEqual(['GET comparisons/c1']);
});

test('stopAll stops following and asks the PC to stop what is not final', async () => {
  const pc = script({ 'POST comparisons/c1/cancel': [[200, snap('c1', 'failed')]], 'POST comparisons/c4/cancel': ['drop'] });
  const base = ask();
  const withCompare = (id: string, over: Partial<NonNullable<Turn['compare']>> = {}): Turn => ({
    ...base,
    id,
    compare: { ...base.compare!, id: `c${id}`, ...over },
  });
  const turns: Turn[] = [
    withCompare('1', { snap: snap('c1', 'running') }),
    withCompare('2', { snap: snap('c2', 'done') }),
    withCompare('3', { error: 'comparison_not_found' }),
    withCompare('4'),
    withCompare('5', { snap: snap('c0', 'done') }), // the snapshot of an older comparison
    { ...base, id: '6', compare: undefined },
  ];
  pc.routes['POST comparisons/c5/cancel'] = [[200, snap('c5', 'failed')]];
  const follows = new Map([['1', new AbortController()], ['1/compare', new AbortController()]]);
  const ctls = [...follows.values()];
  stopAll(pc.client, turns, follows);
  expect(follows.size).toBe(0);
  expect(ctls.every((c) => c.signal.aborted)).toBe(true);
  expect(said(pc.calls)).toEqual(['POST comparisons/c1/cancel', 'POST comparisons/c4/cancel', 'POST comparisons/c5/cancel']);
  await new Promise((r) => setTimeout(r, 0)); // a failed cancel is let go

  const again = new Map([['x', new AbortController()]]);
  stopAll(undefined, turns, again);
  expect(again.size).toBe(0);
});

// ---- Found in the S5 review (R-34) ----

test("the 202's compareError reaches the turn while the AIs answer, and its words show after", async () => {
  const pc = script({
    'POST runs': [[202, runAt('running', [], { compareError: { code: 'too_many_comparisons', message: 'busy' } })]],
    'GET runs/r1': [[200, runAt('done', answers, { pollAfterMs: undefined })]],
  });
  const h = harness(ask());
  await followRun(pc.client, h.now(), h.deps);
  expect(h.seen.some((x) => x.run?.status === 'running' && x.compare?.error === 'too_many_comparisons')).toBe(true);
  expect(said(pc.calls)).toEqual(['POST runs', 'GET runs/r1']);
  expect(compareState(h.now())).toEqual({ kind: 'failed', code: 'too_many_comparisons', action: 'start' });
});

test('새 질문 while the AIs still answer cancels the comparison the 202 named', async () => {
  let h: ReturnType<typeof harness> | undefined;
  const pc = script({
    'POST runs': [[202, runAt('running', [], { comparisonId: 'c1' })]],
    'GET runs/r1': [
      () => {
        // 새 질문 is pressed while the next look at the run is on its way.
        stopAll(pc.client, [h!.now()], h!.deps.follows);
        return [200, runAt('running', [answers[0]], { comparisonId: 'c1' })];
      },
    ],
    'POST comparisons/c1/cancel': [[200, snap('c1', 'failed')]],
  });
  h = harness(ask());
  await followRun(pc.client, h.now(), h.deps);
  expect(said(pc.calls)).toEqual(['POST runs', 'GET runs/r1', 'POST comparisons/c1/cancel']);
  expect(h.deps.follows.size).toBe(0);
});

test('newTurn sends only the asked AIs that judge, and earlier waits only for them', () => {
  const info4 = infoOf([judge('claude', 'anthropic'), judge('codex', 'openai'), judge('opencode', 'deepseek'), judge('gemini', 'google', ['ask'])]);
  const on = [...all, 'gemini'];
  const first = { ...ask({ id: 't0', question: 'first', on, info: info4 }), run: runAt('done', [...answers, { agent: 'gemini', status: 'failed' }]) };
  const t = ask({ id: 't1', on, turns: [first], info: info4 });
  expect(t.agents).toEqual(on);
  expect(t.compare).toEqual({ start: 'auto', judges: all, earlier: ['first'] });
  expect(t.req.compare).toEqual({ earlier: ['first'], judges: all });
});

test('two presses of 비교하기 before the screen redraws send one comparison', async () => {
  const pc = script({
    'POST comparisons': [[202, snap('c1', 'waiting', { source: 'direct' })]],
    'GET comparisons/c1': [[200, snap('c1', 'done', { source: 'direct' })]],
  });
  const h = harness({ ...ask({ auto: false }), run: runAt('done', answers) });
  // The screen hands both presses the turn it last drew.
  const drawn = h.now();
  await Promise.all([followCompare(pc.client, drawn, 'start', h.deps), followCompare(pc.client, drawn, 'start', h.deps)]);
  expect(said(pc.calls)).toEqual(['POST comparisons', 'GET comparisons/c1']);
  expect(pc.calls[0].body?.clientComparisonId).toBe('key-1');
  expect(h.now().compare).toMatchObject({ id: 'c1', key: undefined, error: undefined });
  expect(compareState(h.now())).toEqual({ kind: 'done', partial: false, retry: false });
  expect(h.deps.follows.size).toBe(0);
});

// ---- S5b: the combined answer ----

const writes = (a: AdapterInfo): AdapterInfo => ({ ...a, capabilities: [...a.capabilities, 'synthesize'] });
const info3w = infoOf([judge('claude', 'anthropic'), judge('codex', 'openai'), judge('opencode', 'deepseek')].map(writes));
const written = { status: 'done', agent: 'codex', model: 'gpt-6.1-terra', ms: 15200, text: 'All in one.', judged: true, unnamedSides: 0 };

test('newTurn on a PC that writes asks for the combined answer with the compare, fixed at send time', () => {
  const t = ask({ info: info3w });
  expect(t.req.compare).toEqual({ earlier: [], judges: all, synthesis: { agent: 'claude' } });
  expect(t.compare).toEqual({ start: 'auto', judges: all, earlier: [], synth: { agent: 'claude' } });
  expect(ask({ info: info3w, synth: { on: true, agent: 'codex' } }).req.compare?.synthesis).toEqual({ agent: 'codex' });
  const off = ask({ info: info3w, synth: { on: false, agent: 'codex' } });
  expect(off.req.compare).toEqual({ earlier: [], judges: all, synthesis: { off: true } });
  expect(off.compare?.synth).toEqual({ off: true });
  // An AI the PC does not list is not named (the PC would refuse the whole compare): the PC picks.
  const gone = ask({ info: info3w, synth: { on: true, agent: 'gemini' } });
  expect(gone.req.compare?.synthesis).toEqual({});
  expect(gone.compare?.synth).toEqual({});
  // Manual compare: nothing goes with the run, 비교하기 sends it.
  const manual = ask({ info: info3w, auto: false });
  expect('compare' in manual.req).toBe(false);
  expect(manual.compare?.synth).toEqual({ agent: 'claude' });
  // No comparison, no combined answer: two AIs (결정 5), one AI.
  expect(ask({ info: info3w, on: ['claude', 'codex'] }).compare).toEqual({ start: 'noJudgeOff', judges: [], earlier: [] });
  expect(ask({ info: info3w, to: { agent: 'codex' } }).compare).toBeUndefined();
  // A PC that does not write (an S5 agent), or whose only writer is not available: the S5 bodies.
  expect(ask().req.compare).toEqual({ earlier: [], judges: all });
  expect(ask().compare).toEqual({ start: 'auto', judges: all, earlier: [] });
  const away = infoOf([judge('claude', 'anthropic'), judge('codex', 'openai'), { ...writes(judge('opencode', 'deepseek')), available: false }]);
  expect(ask({ info: away }).req.compare).toEqual({ earlier: [], judges: ['claude', 'codex'] });
});

test('a run with a comparison is followed until its combined answer is written', async () => {
  const pc = script({
    'POST runs': [[202, runAt('running', [], { comparisonId: 'c1' })]],
    'GET runs/r1': [[200, runAt('done', answers, { comparisonId: 'c1', pollAfterMs: undefined })]],
    'GET comparisons/c1': [
      [200, snap('c1', 'running', { synthesis: { status: 'waiting', agent: 'claude', judged: false } })],
      [200, snap('c1', 'done', { pollAfterMs: 1, synthesis: { status: 'running', agent: 'codex', judged: true } })],
      [200, snap('c1', 'done', { synthesis: written })],
    ],
  });
  const h = harness(ask({ info: info3w }));
  await followRun(pc.client, h.now(), h.deps);
  expect(said(pc.calls)).toEqual(['POST runs', 'GET runs/r1', 'GET comparisons/c1', 'GET comparisons/c1', 'GET comparisons/c1']);
  expect(pc.calls[0].body?.compare).toEqual({ earlier: [], judges: all, synthesis: { agent: 'claude' } });
  const kinds = h.seen.filter((x) => x.compare?.snap).map((x) => `${compareState(x)?.kind}/${synthState(x, info3w)?.kind}`);
  expect(kinds).toEqual(['comparing/waiting', 'done/writing', 'done/done']);
  expect(synthState(h.now(), info3w)).toEqual({ kind: 'done', text: 'All in one.', agent: 'codex', model: 'gpt-6.1-terra', ms: 15200, judged: true, unnamed: 0 });
  expect(h.deps.follows.size).toBe(0);
});

test("'synth' asks the PC to write the combined answer and follows it; 409 is just followed", async () => {
  const t = ask({ info: info3w, synth: { on: false, agent: 'claude' } });
  const off = snap('c1', 'done', { synthesis: { status: 'off', judged: false } });
  const pc = script({
    'POST comparisons/c1/synthesis': [[202, snap('c1', 'done', { pollAfterMs: 1, synthesis: { status: 'waiting', agent: 'codex', judged: false } })]],
    'GET comparisons/c1': [
      [200, snap('c1', 'done', { pollAfterMs: 1, synthesis: { status: 'running', agent: 'codex', judged: true } })],
      [200, snap('c1', 'done', { synthesis: written })],
    ],
  });
  const h = harness({ ...t, run: runAt('done', answers), compare: { ...t.compare!, id: 'c1', snap: off } });
  expect(synthState(h.now(), info3w)).toEqual({ kind: 'off', cost: 1 });
  await followCompare(pc.client, h.now(), 'synth', h.deps, { agent: 'codex' });
  expect(said(pc.calls)).toEqual(['POST comparisons/c1/synthesis', 'GET comparisons/c1', 'GET comparisons/c1']);
  expect(pc.calls[0].body).toEqual({ agent: 'codex' });
  expect(h.seen.map((x) => synthState(x, info3w)?.kind)).toEqual(['off', 'waiting', 'writing', 'done']);
  // While it was written the comparison stayed done, with no 다시 비교.
  expect(h.seen.every((x) => JSON.stringify(compareState(x)) === JSON.stringify({ kind: 'done', partial: false, retry: false }))).toBe(true);

  // Still being written (another device, or a press on its way): just follow it. No writer: {}.
  pc.routes['POST comparisons/c1/synthesis'] = [[409, { error: { code: 'comparison_running', message: 'running' } }]];
  pc.routes['GET comparisons/c1'] = [[200, snap('c1', 'done', { synthesis: written })]];
  await followCompare(pc.client, h.now(), 'synth', h.deps);
  expect(said(pc.calls).slice(3)).toEqual(['POST comparisons/c1/synthesis', 'GET comparisons/c1']);
  expect(pc.calls[3].body).toEqual({});
  expect(h.now().compare?.error).toBeUndefined();

  // Any other refusal is kept as a code on the turn.
  pc.routes['POST comparisons/c1/synthesis'] = [[404, { error: { code: 'comparison_not_found', message: 'gone' } }]];
  await followCompare(pc.client, h.now(), 'synth', h.deps, { agent: 'codex' });
  expect(said(pc.calls).slice(5)).toEqual(['POST comparisons/c1/synthesis']);
  expect(h.now().compare?.error).toBe('comparison_not_found');
  expect(compareState(h.now())).toEqual({ kind: 'notFound' });
  expect(h.deps.follows.size).toBe(0);
});

test('stopAll also stops a comparison whose combined answer is still being written', () => {
  const pc = script({ 'POST comparisons/c1/cancel': [[200, snap('c1', 'done', { synthesis: { status: 'running', judged: true } })]] });
  const base = ask({ info: info3w });
  const turns: Turn[] = [
    { ...base, id: '1', compare: { ...base.compare!, id: 'c1', snap: snap('c1', 'done', { pollAfterMs: 1, synthesis: { status: 'running', judged: true } }) } },
    { ...base, id: '2', compare: { ...base.compare!, id: 'c2', snap: snap('c2', 'done', { synthesis: written }) } },
    { ...base, id: '3', compare: { ...base.compare!, id: 'c3', snap: snap('c3', 'failed', { synthesis: { status: 'off', judged: false } }) } },
  ];
  stopAll(pc.client, turns, new Map());
  expect(said(pc.calls)).toEqual(['POST comparisons/c1/cancel']);
});
