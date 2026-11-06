// P2-5 토론: when a turn offers a debate, what it sends, its state and words.

import type { Debate, Info } from '@/api/types';
import { setLang } from '@/i18n';

import {
  debateCost,
  debateErrorText,
  debateOffered,
  debateRequest,
  debateView,
  debating,
  duration,
  foldText,
  natureLabel,
  resultLabel,
  sideLine,
  stanceLabel,
} from './debate';
import type { Turn } from './thread';

afterEach(() => setLang('en'));

const info = (debate = true) =>
  ({
    agents: ['claude', 'codex', 'opencode'].map((agent) => ({ agent, available: true, capabilities: ['ask', 'synthesize'] })),
    capabilities: ['ask', 'synthesize'],
    ...(debate ? { debate: { maxPoints: 2, maxTurns: 4, rules: 'debate-v4' } } : {}),
  }) as unknown as Info;

function turn(o: { synth?: string; agents?: string[]; tier?: string; debate?: Turn['debate'] } = {}): Turn {
  const agents = o.agents ?? ['claude', 'codex', 'opencode'];
  return {
    id: 't1',
    question: 'When is the first ferry?',
    req: { mode: 'ask', prompt: 'q', tier: (o.tier ?? 'deep') as 'deep', agents: agents.map((agent) => ({ agent })) },
    agents,
    run: {
      runId: 'r1',
      mode: 'ask',
      status: 'done',
      startedAt: 'x',
      parts: agents.map((agent) => ({ agent, status: 'done', answer: `${agent} answer`, model: `${agent}-m` })),
    },
    compare: {
      start: 'auto',
      judges: agents,
      earlier: ['before?'],
      synth: { agent: 'codex' },
      id: 'c1',
      snap: { comparisonId: 'c1', status: 'done', source: 'run', engine: 'pc', synthesis: { status: o.synth ?? 'done', agent: 'codex', text: 'Combined.' } },
    },
    debate: o.debate,
  } as unknown as Turn;
}

const snap = (over: Partial<Debate>): Debate => ({ debateId: 'd1', status: 'debating', points: [], calls: 0, ms: 0, pollAfterMs: 1000, ...over });

test('a debate is offered on a done combined answer of two or more answers, where the PC runs debates', () => {
  expect(debateOffered(turn(), info())).toBe(true);
  expect(debateOffered(turn(), info(false))).toBe(false); // an older agent
  expect(debateOffered(turn(), null)).toBe(false);
  expect(debateOffered(turn({ synth: 'running' }), info())).toBe(false);
  expect(debateOffered(turn({ agents: ['claude'] }), info())).toBe(false);
  // One on its way or running: not again; one that failed for a reason a new one may fix: again.
  expect(debateOffered(turn({ debate: { key: 'k' } }), info())).toBe(false);
  expect(debateOffered(turn({ debate: { id: 'd1', snap: snap({}) } }), info())).toBe(false);
  expect(debateOffered(turn({ debate: { id: 'd1', snap: snap({ status: 'done', pollAfterMs: undefined }) } }), info())).toBe(false);
  expect(debateOffered(turn({ debate: { id: 'd1', snap: snap({ status: 'failed', pollAfterMs: undefined, error: { code: 'judge_bad_output', message: '' } }) } }), info())).toBe(true);
  expect(debateOffered(turn({ debate: { id: 'd1', snap: snap({ status: 'failed', pollAfterMs: undefined, error: { code: 'need_two_answers', message: '' } }) } }), info())).toBe(false);
  expect(debateOffered(turn({ debate: { error: 'too_many_debates' } }), info())).toBe(true);
  // A stopped debate may be started again (11-07).
  expect(debateOffered(turn({ debate: { id: 'd1', snap: snap({ status: 'stopped', pollAfterMs: undefined }) } }), info())).toBe(true);
  expect(debateCost(turn())).toBe(10);
  expect(debateCost(turn({ agents: ['claude', 'codex'] }))).toBe(8);
});

test('the request carries the answers, the combined answer, its writer, the tier and the earlier questions', () => {
  expect(debateRequest(turn(), info(), 'k1')).toEqual({
    clientDebateId: 'k1',
    question: 'When is the first ferry?',
    earlier: ['before?'],
    judges: ['claude', 'codex', 'opencode'],
    answers: ['claude', 'codex', 'opencode'].map((agent) => ({ agent, via: 'pc', model: `${agent}-m`, text: `${agent} answer` })),
    synthesis: 'Combined.',
    moderator: 'codex',
    tier: 'deep',
  });
  expect(debateRequest(turn({ synth: 'failed' }), info(), 'k')).toBeNull();
  // A long answer is cut to the agent's 64 KiB of UTF-8.
  const long = turn();
  long.run!.parts[0].answer = '가'.repeat(30000);
  expect(new TextEncoder().encode(debateRequest(long, info(), 'k')!.answers[0].text).length).toBeLessThanOrEqual(65536);
  // 11-06: the moderator model the turn asked for its writer, when that AI wrote.
  const own = turn();
  own.compare!.synth = { agent: 'codex', model: 'gpt-6.1-sol', effort: 'high' };
  expect(debateRequest(own, info(), 'k')).toMatchObject({ moderator: 'codex', moderatorModel: 'gpt-6.1-sol', moderatorEffort: 'high' });
  own.compare!.synth = { agent: 'claude', model: 'opus' }; // asked for Claude, Codex wrote
  expect(debateRequest(own, info(), 'k')).not.toHaveProperty('moderatorModel');
});

test('the view follows the PC', () => {
  expect(debateView(turn())).toBeNull();
  expect(debateView(turn({ debate: { key: 'k' } }))?.kind).toBe('starting');
  expect(debateView(turn({ debate: { key: 'k', error: 'network' } }))).toEqual({ kind: 'lost', action: 'start' });
  expect(debateView(turn({ debate: { id: 'd1', error: 'network' } }))).toEqual({ kind: 'lost', action: 'poll' });
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'planning' }) } }))?.kind).toBe('planning');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({}) } }))?.kind).toBe('running');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'writing' }) } }))?.kind).toBe('writing');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'done', pollAfterMs: undefined }) } }))?.kind).toBe('none');
  const done = snap({ status: 'done', pollAfterMs: undefined, after: { text: 'A', agent: 'claude', ms: 1 } });
  expect(debateView(turn({ debate: { id: 'd1', snap: done } }))?.kind).toBe('done');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'stopped', pollAfterMs: undefined }) } }))?.kind).toBe('stopped');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'failed', pollAfterMs: undefined, error: { code: 'no_judge', message: '' } }) } }))).toMatchObject({
    kind: 'failed',
    code: 'no_judge',
    again: false,
  });
  // A snapshot of another debate is not this one's.
  expect(debateView(turn({ debate: { id: 'd2', snap: snap({}) } }))?.kind).toBe('starting');
  // A status this app does not know: running while the PC polls, else failed.
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'thinking' }) } }))?.kind).toBe('running');
  expect(debateView(turn({ debate: { id: 'd1', snap: snap({ status: 'thinking', pollAfterMs: undefined }) } }))?.kind).toBe('failed');
  expect(debating(turn({ debate: { id: 'd1', snap: snap({}) } }))).toBe(true);
  expect(debating(turn({ debate: { id: 'd1', snap: done } }))).toBe(false);
});

test('the chips and lines in English and Korean', () => {
  expect([natureLabel('fact'), stanceLabel('partly'), resultLabel('conditional'), natureLabel('vibes'), stanceLabel(undefined)]).toEqual([
    'Fact',
    'Partly changes',
    'Depends',
    '',
    '',
  ]);
  expect(sideLine({ ais: ['codex', 'opencode'], says: '8 a.m.' })).toBe('Codex, OpenCode: 8 a.m.');
  expect([duration(38000), duration(146000)]).toEqual(['38 s', '2 min 26 s']);
  expect(debateErrorText('too_many_debates')).toMatch(/^Another debate is running/);
  expect(debateErrorText('need_two_answers')).toBeTruthy(); // the comparison's words
  setLang('ko');
  expect([natureLabel('taste'), stanceLabel('change'), resultLabel('differ'), duration(146000)]).toEqual(['취향', '바꿈', '여전히 다름', '2분 26초']);
});

// R-73 (S10, 11-06): a folded turn showed its Markdown as typed.
test('a folded turn reads as plain words', () => {
  expect(foldText("**Mapo Dalbit Bakery**'s strongest point is `Mapo`; see [the map](https://example.com) and {{Claude}}.")).toBe(
    "Mapo Dalbit Bakery's strongest point is Mapo; see the map and Claude.",
  );
  expect(foldText('## Pick\n__one__ of them')).toBe('Pick\none of them');
});
