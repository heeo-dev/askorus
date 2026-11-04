// S5c: what the Ask screen shows of a turn (present.ts), on snapshots built
// with the pretend agent's judging (test/fakeCompare.ts), and that judging
// itself: every judged sentence now shows on a card (R-44).

import type { CompareAnswer, Comparison, Info, JudgeRequest, RunPart, Synthesis } from '@/api/types';
import { setLang } from '@/i18n';
import { aggregate, judgePair, splitUnits } from '@/test/fakeCompare';

import { cards, type ItemView, type JudgmentLine, type UnitView } from './compare';
import {
  aiStatus,
  answeredAgents,
  blockTitle,
  cardPreview,
  cutText,
  differBundle,
  differHeadline,
  differRows,
  displayModel,
  evidenceSections,
  groupJudgments,
  judgeGroups,
  metaLine,
  numbersOf,
  partError,
  partViews,
  plainPreview,
  rowValue,
  sectionCaption,
  steps,
  synthExpected,
  tiles,
  turnTimes,
  whoOf,
} from './present';
import type { Turn, TurnCompare } from './thread';

afterEach(() => setLang('en'));

// ---- Fixtures ----

// Claude says 9.4 million, Codex and OpenCode 9.7 million: one Differ item,
// one Agree item (the capital), two Only one items.
const texts = {
  claude: 'Seoul is the capital. It has 9.4 million people. Try the palace.',
  codex: 'Seoul is the capital. It has 9.7 million people.',
  opencode: 'Seoul is the capital. It has 9.7 million people. Eat at the market.',
};
const judgeModel: Record<string, string> = { claude: 'claude-haiku-5-5', codex: 'gpt-6-luna', opencode: 'opencode-go/deepseek-v4.1-flash#high' };
const third = (a: string, b: string) => ['claude', 'codex', 'opencode'].find((j) => j !== a && j !== b) ?? '';

function answersOf(t: Record<string, string>): CompareAnswer[] {
  return Object.entries(t).map(([agent, text]) => ({
    id: agent,
    agent,
    via: 'pc',
    family: agent,
    status: 'ready',
    notJudged: 0,
    units: splitUnits(agent, text, 40),
  }));
}

/**
 * comparison judges every ordered pair with the third AI: the first `ended`
 * requests ended (as replies says: ok, unsure or fail), the rest run.
 */
function comparison(
  o: { texts?: Record<string, string>; ended?: number; core?: boolean; replies?: Record<string, 'ok' | 'unsure' | 'fail'>; synthesis?: Synthesis } = {},
): Comparison {
  const answers = answersOf(o.texts ?? texts);
  const pairs: Array<[CompareAnswer, CompareAnswer]> = [];
  for (const a of answers) for (const b of answers) if (a !== b) pairs.push([a, b]);
  const ended = o.ended ?? pairs.length;
  const requests: JudgeRequest[] = pairs.map(([a, b], i) => {
    const judge = third(a.agent, b.agent);
    const r: JudgeRequest = { id: `r${i + 1}`, from: a.id, against: b.id, judge, judgeOnly: false, model: judgeModel[judge], status: 'running', attempt: 1 };
    if (i < ended) {
      r.ms = 1000 * (i + 1);
      if (o.replies?.[judge] === 'fail') Object.assign(r, { status: 'failed', error: { code: 'judge_failed', message: 'usage limit reached' } });
      else r.status = 'done';
    }
    return r;
  });
  const judgments = requests
    .filter((r) => r.status === 'done')
    .flatMap((r) =>
      judgePair(answers.find((a) => a.id === r.from) as CompareAnswer, answers.find((a) => a.id === r.against) as CompareAnswer, {
        request: r.id,
        judge: r.judge,
        unsure: o.replies?.[r.judge] === 'unsure',
        core: o.core ?? true,
      }),
    );
  const all = requests.every((r) => r.status === 'done' || r.status === 'failed');
  const { items, counts } = aggregate(answers, requests, judgments, all);
  const writing = o.synthesis?.status === 'waiting' || o.synthesis?.status === 'running';
  const s: Comparison = {
    comparisonId: 'cmp-1',
    status: all ? (requests.some((r) => r.status === 'done') ? 'done' : 'failed') : 'running',
    source: 'run',
    engine: 'pc',
    answers,
    requests,
    judgments,
    items,
    counts,
  };
  if (!all || writing) s.pollAfterMs = 1000;
  if (o.synthesis) s.synthesis = o.synthesis;
  return s;
}

const part = (agent: string, over: Partial<RunPart> = {}): RunPart => ({
  agent,
  status: 'done',
  model: `${agent}-std`,
  answer: texts[agent as keyof typeof texts],
  ms: 2000,
  ...over,
});

function turn(o: { parts?: RunPart[]; compare?: Partial<TurnCompare> | null; snap?: Comparison; error?: string; running?: boolean } = {}): Turn {
  const agents = ['claude', 'codex', 'opencode'];
  const t: Turn = {
    id: 't1',
    question: 'Tell me about Seoul',
    req: { mode: 'ask', prompt: 'Tell me about Seoul', tier: 'standard' },
    agents,
    at: 1_000_000,
    run: {
      runId: 'run-1',
      mode: 'ask',
      status: o.running ? 'running' : 'done',
      startedAt: 'x',
      parts: o.parts ?? agents.map((a) => part(a)),
    },
  };
  if (o.error) t.error = o.error;
  if (o.compare !== null) {
    t.compare = { start: 'auto', judges: agents, earlier: [], synth: { agent: 'claude' }, id: 'cmp-1', ...o.compare };
    if (o.snap) t.compare.snap = o.snap;
  }
  return t;
}

const info = (verified = false): Info => ({
  name: 'asksemble-agent',
  version: '0.1.0',
  apiVersion: 1,
  hostname: 'PC',
  os: 'windows',
  agents: ['claude', 'codex', 'opencode'].map((agent) => ({ agent, available: true, capabilities: ['ask', 'judge', 'synthesize'] })),
  capabilities: ['ask', 'judge', 'synthesize'],
  reserved: [],
  judging: { engine: 'pc', shape: 'directed', labels: 'v1', rules: 'judge-v1', checks: [], ...(verified ? { verified: { date: '2026-11-01' }, typicalMs: 40444 } : {}) },
});

const waiting: Synthesis = { status: 'waiting', agent: 'claude', judged: false };
const writing: Synthesis = { status: 'running', agent: 'claude', model: 'claude-sonnet-5', judged: true };
const written: Synthesis = { status: 'done', agent: 'claude', model: 'claude-sonnet-5', ms: 15200, judged: true, text: 'Fake combined answer.' };

// ---- Models, times, previews ----

test('a model shows without its variant and provider (R-39)', () => {
  expect(displayModel('opencode-go/deepseek-v4.1-flash#high')).toBe('deepseek-v4.1-flash');
  expect(displayModel('deepseek-v4.1-flash#max')).toBe('deepseek-v4.1-flash');
  expect(displayModel('anthropic/claude-haiku-5-5')).toBe('claude-haiku-5-5');
  expect(displayModel('a/b/c#x#y')).toBe('c');
  expect(displayModel('gpt-6-luna')).toBe('gpt-6-luna');
  expect(displayModel(' sonnet ')).toBe('sonnet');
  expect(displayModel('#high')).toBe('#high'); // nothing left: as given
  expect(displayModel('')).toBe('');
  expect(displayModel(undefined)).toBe('');
  // Every place a turn shows a model uses it.
  const v = partViews(turn({ parts: [part('opencode', { model: 'opencode-go/deepseek-v4.1-flash#high' })] }), 0);
  expect(v.find((p) => p.agent === 'opencode')?.model).toBe('deepseek-v4.1-flash');
});

test('an answer preview is its plain text, cut to two lines worth', () => {
  expect(plainPreview('# Seoul\n\n**Seoul** is the *capital*.\n\n- About 9.4 million\n- [Wiki](https://x.y)')).toBe(
    'Seoul Seoul is the capital. About 9.4 million Wiki',
  );
  expect(plainPreview('> quoted `code $HOME/x:$PATH` here')).toBe('quoted code $HOME/x:$PATH here');
  expect(plainPreview('Area is $\\pi r^2$.')).toBe('Area is πr².');
  expect(plainPreview('| a | b |\n|---|---|\n| 1 | 2 |')).toBe('a b 1 2');
  expect(plainPreview('```js\nlet x = 1;\n```')).toBe('let x = 1;');
  expect(plainPreview('snake_case and 2*3*4 stay; ~~gone~~ marks &amp; <b>tags</b>')).toBe('snake_case and 2*3*4 stay; gone marks & tags');
  const long = plainPreview('가'.repeat(300));
  expect(long).toHaveLength(160);
  expect(long.endsWith('…')).toBe(true);
  expect(cutText('ab😀', 3)).toBe('ab…'); // never half an emoji
  expect(cutText('short', 160)).toBe('short');
});

test('the AIs of a turn: names, models, seconds and a value that is always an object (R-40)', () => {
  const t = turn({
    running: true,
    parts: [part('claude', { ms: 9400 }), part('codex', { status: 'running', answer: undefined, ms: undefined }), part('opencode', { status: 'failed', answer: undefined, error: { code: 'ask_timeout' } })],
  });
  const v = partViews(t, 1_000_000 + 12_400);
  expect(v.map((p) => [p.name, p.model, p.state, p.s])).toEqual([
    ['Claude', 'claude-std', 'done', 9],
    ['Codex', 'codex-std', 'answering', 12],
    ['OpenCode', 'opencode-std', 'failed', 2],
  ]);
  expect(v.map(rowValue)).toEqual([{ text: 'Answered, 9 s' }, { text: 'Answering, 12 s' }, { text: 'Did not answer' }]);
  expect(v[2].error).toBe('No answer within 3 minutes.');
  expect(v.map(aiStatus)).toEqual([
    { tone: 'sub', icon: 'check', text: '9 s' },
    { tone: 'waiting', icon: 'spinner', text: 'Answering…' },
    { tone: 'danger', text: 'Failed · No answer within 3 minutes.' },
  ]);
  expect(v.map(cardPreview)).toEqual([texts.claude, 'Answering…', 'No answer within 3 minutes.']);
  expect(answeredAgents(t)).toEqual(['claude']);

  // A run that could not be followed: the AIs still running did not answer; the turn says why.
  const lost = partViews(turn({ running: true, error: "Can't reach the PC.", parts: [part('claude'), part('codex', { status: 'running' })] }), 0);
  expect(lost.map((p) => [p.state, p.error])).toEqual([
    ['done', undefined],
    ['failed', 'Did not answer'],
    ['failed', 'Did not answer'],
  ]);
  expect(partError({ code: 'ask_failed', message: 'usage limit reached' })).toBe('The AI reported an error.\nusage limit reached');
});

test('an AI still answering shows the model the turn asked for until the PC says which (UX-8)', () => {
  const going = { status: 'running', model: undefined, answer: undefined, ms: undefined } as const;
  const t = turn({ running: true, parts: [part('claude', going), part('codex', going), part('opencode', { model: 'opencode-go/deepseek-v4.1-flash#high' })] });
  // Claude with a model picked in the AI sheet, the others with the tier's.
  t.req = { ...t.req, agents: [{ agent: 'claude', model: 'claude-opus-5#max' }, { agent: 'codex' }, { agent: 'opencode' }] };
  const withTiers = info();
  withTiers.agents = withTiers.agents.map((a) => ({
    ...a,
    ask: { tiers: { quick: { model: `${a.agent}-quick` }, standard: { model: a.agent === 'codex' ? 'openai/gpt-6.1-terra#high' : `${a.agent}-std` } } },
  }));
  expect(partViews(t, 0, withTiers).map((p) => p.model)).toEqual(['claude-opus-5', 'gpt-6.1-terra', 'deepseek-v4.1-flash']);
  // Without the PC's info: the model asked for, else none yet.
  expect(partViews(t, 0).map((p) => p.model)).toEqual(['claude-opus-5', '', 'deepseek-v4.1-flash']);
  // Once the PC says which model answered, that one.
  const done = turn({ parts: [part('claude', { model: 'claude-sonnet-5' }), part('codex', { model: '' }), part('opencode')] });
  done.req = { ...done.req, tier: 'quick' };
  expect(partViews(done, 0, withTiers).map((p) => p.model)).toEqual(['claude-sonnet-5', 'codex-quick', 'opencode-std']);
});

test('times: the slowest answer, and the slowest judging once the judges are done', () => {
  const parts = [part('claude', { ms: 9400 }), part('codex', { ms: 12_400 }), part('opencode', { status: 'failed', ms: 3000, answer: undefined })];
  expect(turnTimes(turn({ parts, snap: comparison({ ended: 3 }) }))).toEqual({ answersMs: 12_400 });
  expect(metaLine(turn({ parts, snap: comparison({ ended: 3 }) }))).toBe('answers 12 s');
  expect(turnTimes(turn({ parts, snap: comparison() }))).toEqual({ answersMs: 12_400, compareMs: 6000 });
  expect(metaLine(turn({ parts, snap: comparison() }))).toBe('answers 12 s · compared in 6 s');
  setLang('ko');
  expect(metaLine(turn({ parts, snap: comparison() }))).toBe('답 12초 · 비교 6초');
});

// ---- The combined answer's steps (R-38) ----

test('while the AIs compare, writing the combined answer is the next step, never the current one (R-38)', () => {
  // The phone check: the snapshot's synthesis waits during the 40 s of comparing, and the old card said 'Combining…'.
  const t = turn({ snap: comparison({ ended: 3, synthesis: waiting }) });
  const st = steps(t, info(true));
  expect(st).toEqual([
    { key: 'compare', label: 'Comparing…', count: '3/6', detail: 'usually 40 s', state: 'active' },
    { key: 'synth', label: 'Writing the combined answer…', state: 'pending' },
  ]);
  expect(st.some((x) => x.key === 'synth' && x.state === 'active')).toBe(false);
  expect(synthExpected(t)).toBe(true);
  expect(blockTitle(t, info())).toBe('Combined answer');
  // Not measured: no typical time.
  expect(steps(t, info())[0]).not.toHaveProperty('detail');
});

test('once the comparison is final the combined answer is the current step, then the steps go', () => {
  expect(steps(turn({ snap: comparison({ synthesis: waiting }) }), info())).toEqual([
    { key: 'compare', label: 'Judgments: 6', state: 'done' },
    { key: 'synth', label: 'Writing the combined answer…', state: 'active' },
  ]);
  expect(steps(turn({ snap: comparison({ synthesis: writing }) }), info()).map((x) => x.state)).toEqual(['done', 'active']);
  // Every judge failed: written from the answers alone.
  const failed = comparison({ replies: { claude: 'fail', codex: 'fail', opencode: 'fail' }, synthesis: writing });
  expect(steps(turn({ snap: failed }), info())).toEqual([
    { key: 'compare', label: 'Comparison', state: 'failed' },
    { key: 'synth', label: 'Writing the combined answer…', state: 'active' },
  ]);
  expect(steps(turn({ snap: comparison({ synthesis: written }) }), info())).toEqual([]);
  expect(blockTitle(turn({ snap: comparison({ synthesis: written }) }), info())).toBe('Combined answer');
});

test('without a combined answer there is one step; before a snapshot the turn says what comes', () => {
  const off: Synthesis = { status: 'off', judged: false };
  const t = turn({ compare: { synth: { off: true } }, snap: comparison({ ended: 2, synthesis: off }) });
  expect(steps(t, info())).toEqual([{ key: 'compare', label: 'Comparing…', count: '2/6', state: 'active' }]);
  expect(synthExpected(t)).toBe(false);
  expect(blockTitle(t, info())).toBe('Comparison');
  expect(steps(turn({ snap: comparison({ synthesis: off }) }), info())).toEqual([]);
  // A comparison on its way (POST /v1/comparisons): no count yet, the combined answer as asked.
  const sending = turn({ compare: { id: undefined, key: 'k1' } });
  expect(steps(sending, info())).toEqual([
    { key: 'compare', label: 'Comparing…', state: 'active' },
    { key: 'synth', label: 'Writing the combined answer…', state: 'pending' },
  ]);
  // Before 비교하기 nothing is coming yet.
  expect(blockTitle(turn({ compare: { start: 'manual', id: undefined } }), info())).toBe('Comparison');
  expect(blockTitle(turn({ compare: { error: 'comparison_not_found' }, snap: comparison({ ended: 3, synthesis: waiting }) }), info())).toBe('Comparison');
  // An older agent: no synthesis at all.
  expect(steps(turn({ compare: { synth: undefined }, snap: comparison({ ended: 1 }) }), info())).toHaveLength(1);
  expect(steps(turn({ compare: null }), info())).toEqual([]);
  setLang('ko');
  expect(steps(turn({ snap: comparison({ ended: 3, synthesis: waiting }) }), info(true))).toEqual([
    { key: 'compare', label: '비교하는 중…', count: '3/6', detail: '보통 40초', state: 'active' },
    { key: 'synth', label: '종합 답변 쓰는 중…', state: 'pending' },
  ]);
});

// ---- Where the AIs differ ----

test('a differ headline names each side and its numbers while short, else how many AIs differ', () => {
  const s = comparison();
  const [item] = cards(s)[0].items;
  expect(differHeadline(item)).toEqual({ text: 'Claude 9.4, Codex·OpenCode 9.7', summary: false });
  expect(numbersOf(item.sides[1].units[0], item.sides[1].numberSpans)).toBe('9.7');
  expect(differRows(s, item).map((r) => [r.agent, r.name, r.unit.text])).toEqual([
    ['claude', 'Claude', 'It has 9.4 million people.'],
    ['codex', 'Codex', 'It has 9.7 million people.'],
    ['opencode', 'OpenCode', 'It has 9.7 million people.'],
  ]);
  // Four sides: the summary, which says it is one (the screen then leaves out 'The AIs differ:', UX-6).
  const four = { ...item, sides: [...item.sides, ...item.sides] };
  expect(differHeadline(four)).toEqual({ text: "The 3 AIs' answers differ", summary: true });
  // A side without numbers: the summary.
  const bare = { ...item, sides: item.sides.map((sd) => ({ ...sd, numberSpans: [] })) };
  expect(differHeadline(bare)).toEqual({ text: "The 3 AIs' answers differ", summary: true });
  setLang('ko');
  expect(differHeadline(bare)).toEqual({ text: 'AI 3개의 답이 다릅니다', summary: true });
});

test('the AI answers differ line: topics by key with a gist per side by position, else the headline or the first words (C5)', () => {
  const s = comparison();
  const items = cards(s)[0].items;
  const key = items[0].key;
  expect(differBundle(s, [], [])).toBeNull();
  // Named: the topic, a gist per side from its position, the AIs as codes.
  const named = differBundle(s, items, [{ key, topic: ' Population ', gists: ['9.4 million', '9.7 million'] }]);
  expect(named).toEqual({
    count: 1,
    topics: ['Population'],
    items: [
      {
        key,
        title: 'Population',
        gists: [
          { agents: ['claude'], text: '9.4 million' },
          { agents: ['codex', 'opencode'], text: '9.7 million' },
        ],
        rows: differRows(s, items[0]),
      },
    ],
  });
  // A gist of a position that is not drawn (a whole answer) or empty is left out.
  expect(differBundle(s, items, [{ key, topic: 'Population', gists: ['', '9.7 million'] }])?.items[0].gists).toEqual([{ agents: ['codex', 'opencode'], text: '9.7 million' }]);
  // Not named (still naming, failed, an older agent, another key): no topics line, the headline.
  for (const n of [undefined, [], [{ key: 'other', topic: 'x', gists: [] }], [{ key, topic: '  ', gists: [] }]]) {
    const b = differBundle(s, items, n);
    expect(b?.topics).toEqual([]);
    expect(b?.items[0]).toMatchObject({ title: 'Claude 9.4, Codex·OpenCode 9.7', gists: [] });
  }
  // A headline that only counts the AIs gives way to the first words of the first sentence, cut with '…'.
  const bare = items.map((it) => ({ ...it, sides: it.sides.map((sd) => ({ ...sd, numberSpans: [] })) }));
  expect(differBundle(s, bare)?.items[0].title).toBe('It has 9.4 million people.');
  const long = bare.map((it) => ({ ...it, sides: it.sides.map((sd) => ({ ...sd, units: sd.units.map((u) => ({ ...u, text: 'Seoul has about 9.3 million people in the city proper.' })) })) }));
  expect(differBundle(s, long)?.items[0].title).toBe('Seoul has about 9.3 million…');
});

test('two numbers of one sentence stay apart in the headline and the who line (UX-2)', () => {
  const claude: UnitView = {
    id: 'claude:1',
    answer: 'claude',
    agent: 'Claude',
    kind: 'sentence',
    text: 'Seoul has about 9.3 million people in the city proper, and the metropolitan area holds over 25 million.',
    math: [],
  };
  const codex: UnitView = { id: 'codex:1', answer: 'codex', agent: 'Codex', kind: 'sentence', text: 'Seoul has about 9.6 million residents.', math: [] };
  const span = (u: UnitView, x: string) => ({ unit: u.id, start: u.text.indexOf(x), end: u.text.indexOf(x) + x.length });
  const claudeSide = { agents: ['Claude'], units: [claude], numberSpans: [span(claude, 'about 9.3 million people'), span(claude, 'over 25 million')] };
  const codexSide = { agents: ['Codex'], units: [codex], numberSpans: [span(codex, 'about 9.6 million residents')] };
  const item: ItemView = { key: 'k', kind: 'differ', sides: [claudeSide, codexSide], numbersDiffer: true, check: [], missing: [], whole: [] };
  expect(numbersOf(claude, claudeSide.numberSpans)).toBe('about 9.3 million people / over 25 million');
  expect(differHeadline(item).text).toBe('Claude about 9.3 million people / over 25 million, Codex about 9.6 million residents');
  expect(whoOf({ ...item, sides: [codexSide, claudeSide] })).toEqual([
    { text: 'Codex ↔ ' },
    { text: 'Claude ' },
    { text: 'about 9.3 million people / over 25 million', bold: true },
  ]);
});

test('a Differ item against a whole answer names that AI and counts it (COR-1)', () => {
  // An opposite with no usable evidence: the agent sends the other answer as a side with no units.
  const s = comparison();
  s.items = [
    {
      key: 'd:claude:2~codex',
      kind: 'differ',
      positions: [
        { agents: ['claude'], units: ['claude:2'], numberSpans: [{ unit: 'claude:2', start: 7, end: 10 }] },
        { agents: ['codex'], units: [] },
      ],
      check: [],
      missing: [],
    },
  ];
  const [item] = cards(s)[0].items;
  expect(item.sides).toHaveLength(1);
  expect(item.whole).toEqual(['Codex']);
  expect(whoOf(item)).toEqual([{ text: 'Claude ↔ ' }, { text: 'Codex' }]);
  // One side's numbers cannot stand for two AIs: the summary, with both counted.
  expect(differHeadline(item)).toEqual({ text: "The 2 AIs' answers differ", summary: true });
  expect(differRows(s, item).map((r) => r.name)).toEqual(['Claude']);
  // With another side too, the whole answer comes after it.
  const [three] = cards({
    ...s,
    items: [{ ...s.items[0], positions: [...s.items[0].positions.slice(0, 1), { agents: ['opencode'], units: ['opencode:2'] }, { agents: ['codex'], units: [] }] }],
  })[0].items;
  expect(whoOf(three)).toEqual([{ text: 'Claude ↔ ' }, { text: 'OpenCode' }, { text: ' · Codex' }]);
  expect(differHeadline(three)).toEqual({ text: "The 3 AIs' answers differ", summary: true });
  setLang('ko');
  expect(differHeadline(item).text).toBe('AI 2개의 답이 다릅니다');
});

// ---- The evidence ----

test('each kind of item says who said it; differ items name the other sides with their numbers', () => {
  const cs = cards(comparison({ replies: { opencode: 'unsure' } }));
  const of = (kind: string) => cs.find((c) => c.kind === kind)?.items ?? [];
  expect(whoOf(of('differ')[0])).toEqual([{ text: 'Claude ↔ ' }, { text: 'Codex, OpenCode ' }, { text: '9.7', bold: true }]);
  expect(whoOf(of('check')[0])).toEqual([{ text: "Claude's sentence · The judging AI was not sure." }]);
  const plain = cards(comparison());
  expect(whoOf(plain[2].items[0])).toEqual([{ text: 'Claude · Codex · OpenCode' }]);
  expect(whoOf(plain[3].items[0])).toEqual([{ text: 'Only Claude' }]);
  // Check this says the numbers differ when every reason is a number check.
  const item = of('check')[0];
  expect(sectionCaption('check', [{ ...item, check: ['numberMismatch'] }, { ...item, check: ['numbersAbsent', 'numberMismatch'] }])).toBe(
    'The numbers differ · check them yourself',
  );
  expect(sectionCaption('check', [{ ...item, check: ['numberMismatch'] }, { ...item, check: ['unsure'] }])).toBe('Check these yourself');
  expect(sectionCaption('check', [])).toBe('Check these yourself');
});

test('the evidence has all four sections: an empty one says so (R-42), three items then more', () => {
  const onMore = jest.fn();
  const onPair = jest.fn();
  const cs = cards(comparison());
  const secs = evidenceSections({ cards: cs, ended: true, partial: false, failed: [], more: [], onMore, onPair });
  expect(secs.map((x) => [x.kind, x.label, x.count, x.caption, x.items.length, x.empty])).toEqual([
    ['differ', 'Differ', 1, 'The AIs said different things', 1, undefined],
    ['check', 'Check this', 0, 'Check these yourself', 0, 'No sentences here.'],
    ['agree', 'Agree', 1, 'That does not make it right', 1, undefined],
    ['onlyOne', 'Only one', 2, 'It may be right or wrong', 2, undefined],
  ]);
  // Differ and Check this lead to the pair sheet; Agree and Only one do not.
  secs[0].items[0].onPress?.();
  expect(onPair).toHaveBeenCalledWith(cs[0].items[0].key);
  expect(secs[2].items[0].onPress).toBeUndefined();
  expect(secs[3].items[0].onPress).toBeUndefined();

  // Seven Only one items: three, then 'Show 4 more', then all.
  const many = cards(comparison({ texts: { claude: 'A one. B two. C three.', codex: 'D four. E five. F six.', opencode: 'Same here.' } }));
  const folded = evidenceSections({ cards: many, ended: true, partial: false, failed: [], more: [], onMore, onPair });
  expect(folded[3].count).toBe(7);
  expect(folded[3].items).toHaveLength(3);
  expect(folded[3].more?.label).toBe('Show 4 more');
  folded[3].more?.onPress();
  expect(onMore).toHaveBeenCalledWith('onlyOne');
  expect(evidenceSections({ cards: many, ended: true, partial: false, failed: [], more: ['onlyOne'], onMore, onPair })[3].items).toHaveLength(7);
  // No Differ item: the S5 words.
  expect(folded[0].empty).toBe('Found no place where the AIs said different things.');
  setLang('ko');
  expect(evidenceSections({ cards: cs, ended: true, partial: false, failed: [], more: [], onMore, onPair })[1].empty).toBe('해당하는 문장이 없습니다.');
});

test('while judges run, the sections besides Differ count “…” and say so; nothing compared says that', () => {
  const run = cards(comparison({ ended: 3 }));
  const secs = evidenceSections({ cards: run, ended: false, partial: false, failed: [], more: [], onMore: jest.fn(), onPair: jest.fn() });
  expect(secs.map((x) => x.count)).toEqual([1, null, null, null]);
  expect(secs.slice(1).map((x) => x.empty)).toEqual(['Comparing…', 'Comparing…', 'Comparing…']);
  expect(tiles(run, false).map((x) => [x.label, x.count, x.a11yLabel])).toEqual([
    ['Differ', 1, 'Differ: 1. Show'],
    ['Check this', null, 'Check this: still comparing'],
    ['Agree', null, 'Agree: still comparing'],
    ['Only one', null, 'Only one: still comparing'],
  ]);
  expect(tiles(cards(comparison()), true).map((x) => x.count)).toEqual([1, 0, 1, 2]);

  const empty = cards({ ...comparison(), items: [] });
  const none = evidenceSections({ cards: empty, ended: true, partial: false, failed: [], more: [], onMore: jest.fn(), onPair: jest.fn() });
  expect(none.map((x) => x.empty)).toEqual(['Found no sentences to compare.', 'No sentences here.', 'No sentences here.', 'No sentences here.']);
  const part = evidenceSections({ cards: cards(comparison({ texts: { claude: 'Same.', codex: 'Same.', opencode: 'Other.' } })), ended: true, partial: true, failed: ['Claude', 'OpenCode'], more: [], onMore: jest.fn(), onPair: jest.fn() });
  expect(part[0].empty).toBe(
    'In the sentences that were compared, found no place where the AIs said different things. Could not compare with the answers of Claude, OpenCode.',
  );
  setLang('ko');
  expect(tiles(run, false)[1].a11yLabel).toBe('확인 필요, 비교하는 중');
  expect(tiles(run, false)[0].a11yLabel).toBe('다른 말 1개, 펼치기');
});

// ---- The pair sheet's judgments (R-41) ----

test('the judgments of one direction are one line, each label once with how often (R-41)', () => {
  const l = (judge: string, from: string, against: string, unit: string, label: string, unsure = false): JudgmentLine => ({ judge, from, against, unit, label, unsure });
  // The phone check: an item with two of Claude's sentences repeated each direction's line.
  const lines = [
    l('OpenCode', 'Claude', 'Codex', 'claude:1', 'opposite'),
    l('OpenCode', 'Claude', 'Codex', 'claude:2', 'opposite'),
    l('Codex', 'Claude', 'OpenCode', 'claude:1', 'opposite'),
    l('Codex', 'Claude', 'OpenCode', 'claude:2', 'same', true),
    l('Claude', 'Codex', 'OpenCode', 'codex:1', 'same'),
    l('Codex', 'Claude', 'OpenCode', 'claude:3', 'same', true),
  ];
  expect(groupJudgments(lines).map((g) => g.text)).toEqual([
    "OpenCode: Claude's sentence vs Codex's answer · Differs ×2",
    "Codex: Claude's sentence vs OpenCode's answer · Differs, Same ×2 · not sure",
    "Claude: Codex's sentence vs OpenCode's answer · Same",
  ]);
  expect(new Set(groupJudgments(lines).map((g) => g.key)).size).toBe(3);
  setLang('ko');
  expect(groupJudgments(lines.slice(0, 2))[0].text).toBe('OpenCode 판정: Claude의 문장 → Codex의 답 · 다른 말 2번');
});

// ---- The judging details sheet ----

test('judging requests go under the AI that judged them, in the turn order, with times and failures', () => {
  const s = comparison({ replies: { codex: 'fail' } });
  s.requests.push({ id: 'r7', from: 'claude', against: 'codex', judge: '', judgeOnly: false, status: 'failed', attempt: 0, error: { code: 'no_judge' } });
  s.requests[0].judgeOnly = true;
  const g = judgeGroups(s, ['claude', 'codex', 'opencode']);
  expect(g.map((x) => [x.agent, x.name, x.model])).toEqual([
    ['claude', 'Claude', 'claude-haiku-5-5'],
    ['codex', 'Codex', 'gpt-6-luna'],
    ['opencode', 'OpenCode', 'deepseek-v4.1-flash'],
    ['', '', ''],
  ]);
  expect(g[0].rows.map((r) => [r.text, r.status, r.failed])).toEqual([
    ['Codex → OpenCode', '4.0 s', false],
    ['OpenCode → Codex', '6.0 s', false],
  ]);
  expect(g[0].rows[0].a11y).toBe("Codex's sentences against OpenCode's answer, 4.0 s");
  expect(g[1].rows[0]).toMatchObject({ text: 'Claude → OpenCode', status: 'The judging AI reported an error.\nusage limit reached', failed: true });
  expect(g[2].rows[0].text).toBe('Claude → Codex (judge only)');
  expect(g[3].rows).toEqual([
    {
      id: 'r7',
      text: 'Claude ↔ Codex',
      a11y: 'Claude ↔ Codex, No AI could judge: it takes an AI of another company that did not write the answers.',
      status: 'No AI could judge: it takes an AI of another company that did not write the answers.',
      failed: true,
    },
  ]);
  // Still judging: waiting or judging, in words.
  const run = comparison({ ended: 3 });
  run.requests[5].status = 'queued';
  const rows = judgeGroups(run, ['claude', 'codex', 'opencode']).flatMap((x) => x.rows);
  expect(rows.map((r) => r.status)).toEqual(['Judging…', 'Waiting', '2.0 s', 'Judging…', '1.0 s', '3.0 s']);
  setLang('ko');
  expect(judgeGroups(s, ['claude'])[0].rows[0].a11y).toBe('Codex의 문장을 OpenCode의 답과, 4.0초');
});

// ---- The pretend agent's cards: every judged sentence shows (R-44) ----

test('judged sentences no judge called core show on the cards, after the core ones, without moving them', () => {
  // All core: as before S5c.
  const core = comparison();
  // None core: the same cards (before S5c only Differ showed).
  const plain = comparison({ core: false });
  const kinds = (s: Comparison) => s.items.map((i) => [i.kind, i.key, i.positions.flatMap((p) => p.units)]);
  expect(kinds(plain)).toEqual(kinds(core));
  expect(plain.counts).toMatchObject({ differ: 1, check: 0, agree: 1, onlyOne: 2 });

  // Some core, some not: the core units' items come first and keep their keys.
  const answers = answersOf(texts);
  const requests: JudgeRequest[] = [];
  const judgments = [];
  let n = 0;
  for (const a of answers)
    for (const b of answers) {
      if (a === b) continue;
      const r: JudgeRequest = { id: `r${++n}`, from: a.id, against: b.id, judge: third(a.agent, b.agent), judgeOnly: false, status: 'done', attempt: 1 };
      requests.push(r);
      // Only Claude's last sentence ('Try the palace.') and OpenCode's capital line are core.
      for (const j of judgePair(a, b, { request: r.id, judge: r.judge, unsure: false, core: false }))
        judgments.push({ ...j, core: j.unit === 'claude:3' || j.unit === 'opencode:1' });
    }
  const mixed = aggregate(answers, requests, judgments, true);
  expect(mixed.items.map((i) => [i.kind, i.key])).toEqual([
    ['differ', 'd:claude:2~codex:2'],
    ['agree', 'a:opencode:1'],
    ['onlyOne', 'o:claude:3'],
    ['onlyOne', 'o:opencode:3'],
  ]);
  // The capital is one Agree item whose sentence is OpenCode's (a core one), with Claude and Codex in it.
  expect(mixed.items[1].positions[0]).toEqual({ agents: ['opencode', 'claude', 'codex'], units: ['opencode:1', 'claude:1', 'codex:1'] });
  // Only the core judgments: what showed before (the non-core sentences only in Differ).
  const before = aggregate(answers, requests, judgments.filter((j) => j.core), true);
  expect(before.items.map((i) => [i.kind, i.key])).toEqual([
    ['agree', 'a:opencode:1'],
    ['onlyOne', 'o:claude:3'],
  ]);
});
