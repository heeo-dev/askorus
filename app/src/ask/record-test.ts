// #29: the conversation record a follow-up carries, built from the turns
// before it; and carry, which sets it on a turn when it goes.

import type { Debate, DebatePoint, Info } from '@/api/types';

import { combining, directRequest } from './compare';
import { debateRequest } from './debate';
import { buildRecord, concluding, finalAnswer, maxSettled, moderatorAnswers, settledOf } from './record';
import { carry, newTurn, type Turn } from './thread';

const info = (context = true) =>
  ({
    agents: ['claude', 'codex'].map((agent) => ({ agent, available: true, capabilities: ['ask', 'synthesize'] })),
    capabilities: ['ask', 'synthesize'],
    debate: { maxPoints: 2, maxTurns: 4, rules: 'debate-v4' },
    ...(context ? { context: { maxSettled: 12, maxLastBytes: 8192 } } : {}),
  }) as unknown as Info;

const point = (over: Partial<DebatePoint>): DebatePoint => ({ id: 'p', topic: 'First ferry', nature: 'fact', debate: true, sides: [], turns: [], ...over });

function turn(id: string, o: { question?: string; synth?: string; agents?: string[]; debate?: Partial<Debate>; noRun?: boolean; queued?: boolean } = {}): Turn {
  const agents = o.agents ?? ['claude', 'codex'];
  const t: Turn = {
    id,
    question: o.question ?? `Question ${id}?`,
    req: { mode: 'ask', prompt: 'q', tier: 'standard', agents: agents.map((agent) => ({ agent })) },
    agents,
  };
  if (!o.noRun) {
    t.run = { runId: `r-${id}`, mode: 'ask', status: 'done', startedAt: 'x', parts: agents.map((agent) => ({ agent, status: 'done', answer: `${agent} answer ${id}` })) };
  }
  if (agents.length > 1) {
    t.compare = {
      start: 'auto',
      judges: agents,
      earlier: [],
      synth: { agent: 'claude' },
      id: `c-${id}`,
      snap: {
        comparisonId: `c-${id}`,
        status: 'done',
        source: 'run',
        engine: 'pc',
        synthesis: o.synth === undefined ? { status: 'done', agent: 'claude', text: `Combined ${id}.` } : { status: o.synth },
      },
    } as Turn['compare'];
  }
  if (o.debate) t.debate = { id: `d-${id}`, snap: { debateId: `d-${id}`, status: 'done', points: [], calls: 5, ms: 1, ...o.debate } as Debate };
  if (o.queued) t.queued = true;
  return t;
}

test('the final answer: after the debate, else the combined answer, else the one answer, else none', () => {
  expect(finalAnswer(turn('1', { debate: { after: { text: 'After 1.', agent: 'claude', ms: 1 } } }))).toBe('After 1.');
  expect(finalAnswer(turn('1', { debate: { status: 'stopped' } }))).toBe('Combined 1.');
  // A debate still running gives nothing of its own.
  expect(finalAnswer(turn('1', { debate: { status: 'debating', after: { text: 'After 1.', agent: 'claude', ms: 1 } } }))).toBe('Combined 1.');
  expect(finalAnswer(turn('1'))).toBe('Combined 1.');
  expect(finalAnswer(turn('1', { agents: ['codex'] }))).toBe('codex answer 1');
  // Several answers with the combined answer off: no final answer.
  expect(finalAnswer(turn('1', { synth: 'off' }))).toBe('');
});

test('settled points: every ended debate, with a result and a line, the latest 12', () => {
  const points = [
    point({ id: 'a', result: 'agreed', line: 'At 8 a.m. ({{Claude}} changed).' }),
    point({ id: 'b', topic: 'Tickets', result: 'kept', line: 'The port or the app.' }),
    point({ id: 'c', topic: 'No result' }),
    point({ id: 'd', topic: 'Odd', result: 'won', line: 'x' }),
    point({ id: 'e', topic: '  ', result: 'differ', line: 'x' }),
  ];
  const turns = [
    turn('1', { debate: { points } }),
    turn('2', { debate: { status: 'stopped', points: [point({ topic: 'Stopped', result: 'conditional', line: 'It depends.' })] } }),
    turn('3', { debate: { status: 'failed', points: [point({ topic: 'Failed', result: 'agreed', line: 'x' })] } }),
  ];
  expect(settledOf(turns)).toEqual([
    { topic: 'First ferry', result: 'agreed', line: 'At 8 a.m. ({{Claude}} changed).' },
    { topic: 'Tickets', result: 'kept', line: 'The port or the app.' },
    { topic: 'Stopped', result: 'conditional', line: 'It depends.' },
  ]);
  const many = Array.from({ length: 15 }, (_, i) => point({ topic: `T${i}`, result: 'agreed', line: `L${i}` }));
  const s = settledOf([turn('1', { debate: { points: many } })]);
  expect(s).toHaveLength(maxSettled);
  expect(s[0].topic).toBe('T3');
  expect(s.at(-1)?.topic).toBe('T14');
});

test('the record: the latest three questions, the settled points and the previous final answer', () => {
  const turns = [
    turn('1', { question: 'First?' }),
    turn('2', { question: 'Second?', debate: { points: [point({ result: 'agreed', line: 'At 8.' })], after: { text: 'After 2.', agent: 'claude', ms: 1 } } }),
    turn('3', { question: 'Third?' }),
    turn('4', { question: 'Fourth?' }),
    // Not sent: it waits, or never reached the PC.
    turn('5', { question: 'Waiting?', queued: true, noRun: true }),
  ];
  expect(buildRecord(turns, info())).toEqual({
    earlier: ['Second?', 'Third?', 'Fourth?'],
    settled: [{ topic: 'First ferry', result: 'agreed', line: 'At 8.' }],
    last: 'Combined 4.',
  });
  // Nothing concluded to carry: none. An older agent: none.
  expect(buildRecord([turn('1', { synth: 'off' })], info())).toBeNull();
  expect(buildRecord([], info())).toBeNull();
  expect(buildRecord(turns, info(false))).toBeNull();
  // The settled points alone still carry.
  expect(buildRecord([turn('1', { synth: 'off', debate: { points: [point({ result: 'differ', line: 'Still apart.' })] } })], info())).toEqual({
    earlier: ['Question 1?'],
    settled: [{ topic: 'First ferry', result: 'differ', line: 'Still apart.' }],
  });
});

test('the record keeps to the agent limits: NULs out, cut by UTF-8 bytes', () => {
  const long = '가'.repeat(5000); // 15,000 bytes
  const t1 = turn('1', { question: `Q\u0000${long}`, debate: { points: [point({ topic: `T${long}`, result: 'agreed', line: `L\u0000${long}` })] } });
  t1.compare!.snap!.synthesis = { status: 'done', agent: 'claude', text: long };
  const r = buildRecord([t1], info())!;
  const bytes = (s: string) => new TextEncoder().encode(s).length;
  expect(r.earlier![0].includes('\u0000') || r.settled![0].line.includes('\u0000')).toBe(false);
  expect(bytes(r.earlier![0])).toBeLessThanOrEqual(4096);
  expect(bytes(r.settled![0].topic)).toBeLessThanOrEqual(512);
  expect(bytes(r.settled![0].line)).toBeLessThanOrEqual(1024);
  expect(bytes(r.last!)).toBeLessThanOrEqual(8192);
  expect(bytes(r.last!)).toBeGreaterThan(8180);
  // A smaller limit from the agent wins.
  const small = { ...info(), context: { maxSettled: 1, maxLastBytes: 30 } } as Info;
  const r2 = buildRecord([turn('1', { debate: { points: [point({ topic: 'A', result: 'agreed', line: 'a' }), point({ topic: 'B', result: 'agreed', line: 'b' })] } })], small)!;
  expect(r2.settled).toEqual([{ topic: 'B', result: 'agreed', line: 'b' }]);
  expect(bytes(r2.last!)).toBeLessThanOrEqual(30);
});

test('carry sets the record when the turn goes; off, or left out this time, sends none', () => {
  const before = [turn('1', { debate: { points: [point({ result: 'agreed', line: 'At 8.' })] } })];
  const next = newTurn({
    id: 't2',
    question: 'And the last?',
    tier: 'standard',
    to: 'all',
    on: ['claude', 'codex'],
    turns: before,
    models: {},
    info: info(),
    synth: { on: true, agent: 'claude' },
    at: 1,
  });
  expect(next.req.context).toBeUndefined();
  const sent = carry(next, before, info(), true);
  expect(sent.req.context).toEqual({ earlier: ['Question 1?'], settled: [{ topic: 'First ferry', result: 'agreed', line: 'At 8.' }], last: 'Combined 1.' });
  expect(next.req.context).toBeUndefined(); // not changed in place
  expect(carry(sent, before, info(), false).req).not.toHaveProperty('context');
  expect(carry({ ...sent, noRecord: true }, before, info(), true).req).not.toHaveProperty('context');
  expect(carry(sent, before, info(false), true).req).not.toHaveProperty('context');

  // A comparison made again and a debate of that turn carry the record it went with.
  const done = { ...turn('2'), req: { ...turn('2').req, context: sent.req.context } };
  expect(directRequest(done, 'k')?.context).toEqual(sent.req.context);
  expect(debateRequest(done, info(), 'k')?.context).toEqual(sent.req.context);
  expect(directRequest(turn('2'), 'k')).not.toHaveProperty('context');
  expect(debateRequest(turn('2'), info(), 'k')).not.toHaveProperty('context');
});

test('what a turn concludes is still coming: its combined answer, then its debate (11-07)', () => {
  const writing = turn('1');
  writing.compare!.snap = { comparisonId: 'c-1', status: 'done', source: 'run', engine: 'pc', pollAfterMs: 1000, synthesis: { status: 'running' } };
  expect(combining(writing)).toBe(true);
  expect(concluding([turn('0'), writing])).toBe('synth');
  // The run ended and the PC's next answer is not in yet: coming, when a combined answer was asked for.
  const noSnap = { ...turn('2'), compare: { ...turn('2').compare!, snap: undefined } } as Turn;
  expect(combining(noSnap)).toBe(true);
  expect(combining({ ...noSnap, compare: { ...noSnap.compare!, synth: { off: true } } } as Turn)).toBe(false);
  // A comparison request on its way.
  expect(combining({ ...turn('3'), compare: { ...turn('3').compare!, key: 'k' } } as Turn)).toBe(true);
  // Done, failed, still answering, an error, one AI: nothing coming.
  expect(combining(turn('4'))).toBe(false);
  expect(combining({ ...turn('5'), run: { ...turn('5').run!, status: 'running' } } as Turn)).toBe(false);
  expect(combining({ ...turn('6'), compare: { ...turn('6').compare!, error: 'network' } } as Turn)).toBe(false);
  expect(combining(turn('7', { agents: ['codex'] }))).toBe(false);
  // A running debate comes first.
  expect(concluding([writing, turn('8', { debate: { status: 'debating', pollAfterMs: 1000 } })])).toBe('debate');
  expect(concluding([turn('9')])).toBeNull();
});

// #25: a question to the moderator reads the last answers to the AIs and always the record.
test('the moderator reads the last answers to the AIs (not its own), cut to 8 KiB, and always the record', () => {
  const t1 = turn('1');
  t1.run!.parts[0].answer = '가'.repeat(5000); // 15,000 bytes
  const mod = { ...turn('2', { agents: ['claude'] }), moderator: true } as Turn;
  const got = moderatorAnswers([t1, mod]);
  expect(got.map((a) => a.agent)).toEqual(['claude', 'codex']);
  expect(new TextEncoder().encode(got[0].text).length).toBeLessThanOrEqual(8192);
  expect(got[1].text).toBe('codex answer 1');
  expect(moderatorAnswers([])).toEqual([]);

  const next = newTurn({
    id: 't3',
    question: 'What did they differ on?',
    tier: 'quick',
    to: { agents: ['claude'] },
    on: ['claude', 'codex'],
    turns: [t1, mod],
    models: { claude: 'opus' },
    info: info(),
    synth: { on: true, agent: 'claude' },
    moderator: { agent: 'claude', model: 'claude-sonnet', effort: 'high', answers: got },
    at: 1,
  });
  expect(next.moderator).toBe(true);
  expect(next.compare).toBeUndefined();
  expect(next.req).toMatchObject({ agents: [{ agent: 'claude', model: 'claude-sonnet', effort: 'high' }], tier: 'quick', moderator: { agent: 'claude' } });
  expect(next.req.agents?.[0].session).toBeUndefined();
  // The record goes even with the setting off, or left out this time.
  expect(carry(next, [t1, mod], info(), false).req.context).toMatchObject({ earlier: ['Question 1?', 'Question 2?'] });
  expect(carry({ ...next, noRecord: true }, [t1, mod], info(), false).req.context).toBeTruthy();
});
