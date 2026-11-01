import { AgentError, CancelledError, NetworkError } from '@/api/client';
import type { AdapterInfo, CardItem, Comparison, Info, JudgeRequest, Judgment, Run, RunPart } from '@/api/types';

import {
  answered,
  answeringLines,
  canRetry,
  cards,
  chipCounts,
  compareErrorText,
  compareStart,
  compareState,
  cutUtf8,
  directRequest,
  earlierFor,
  errorCode,
  failedWith,
  isFinal,
  judgedBy,
  judgeModels,
  judgesOf,
  labelText,
  longAnswers,
  pairView,
  pieces,
  polling,
  progress,
  requestLines,
  shownCards,
  startCost,
  tabs,
  usableJudgments,
  utf8Bytes,
  whyText,
  withError,
  withRun,
  withSnap,
} from './compare';
import type { Turn, TurnCompare } from './thread';

// ---- Fixtures ----

const ai = (agent: string, family: string, caps = ['ask', 'judge'], available = true): AdapterInfo => ({
  agent,
  available,
  capabilities: caps,
  ...(caps.includes('judge') ? { judge: { model: `${agent}-quick`, family } } : {}),
});

const infoOf = (agents: AdapterInfo[]): Info => ({
  name: 'asksemble-agent',
  version: '0.3.0',
  apiVersion: 1,
  hostname: 'pc',
  os: 'windows',
  agents,
  capabilities: ['ask'],
  reserved: [],
});

const three = infoOf([ai('claude', 'anthropic'), ai('codex', 'openai'), ai('opencode', 'deepseek')]);
const all = ['claude', 'codex', 'opencode'];

const done = (agent: string, answer: string, model: string | null = `${agent}-standard`): RunPart =>
  model ? { agent, status: 'done', answer, model } : { agent, status: 'done', answer };
const runOf = (status: string, parts: RunPart[], over: Partial<Run> = {}): Run => ({
  runId: 'r1',
  mode: 'ask',
  status,
  startedAt: 'x',
  parts,
  ...over,
});
const tc = (over: Partial<TurnCompare> = {}): TurnCompare => ({ start: 'auto', judges: all, earlier: [], ...over });
const turnOf = (over: Partial<Turn> = {}): Turn => ({
  id: 't1',
  question: 'Capital?',
  req: { mode: 'ask', prompt: 'Capital?' },
  agents: all,
  ...over,
});
const answers3 = [
  done('claude', 'Seoul is the capital. It has 9.4 million people. Try the palace.'),
  done('codex', 'Seoul is the capital. It has 9.7 million people.'),
  done('opencode', 'Seoul is the capital. It has 9.7 million people. Eat at the market.'),
];
const doneTurn = (compare: Partial<TurnCompare> = {}, parts = answers3) => turnOf({ run: runOf('done', parts), compare: tc(compare) });

const unit = (id: string, n: number, text: string, over: object = {}) => ({ id, n, kind: 'sentence', text, start: 0, end: text.length, judged: true, ...over });
const req = (id: string, from: string, against: string, judge: string, over: Partial<JudgeRequest> = {}): JudgeRequest => ({
  id,
  from,
  against,
  judge,
  judgeOnly: false,
  model: judge ? `${judge}-quick` : undefined,
  status: 'done',
  attempt: 1,
  ...over,
});
const judgment = (unitId: string, against: string, judge: string, label = 'same', over: Partial<Judgment> = {}): Judgment => ({
  unit: unitId,
  against,
  request: 'r1',
  judge,
  core: true,
  label,
  evidence: [],
  unsure: false,
  flags: [],
  ...over,
});
const counts = { differ: 0, check: 0, agree: 0, onlyOne: 0, notCompared: 0, notJudged: 0 };

const differItem: CardItem = {
  key: 'd:claude:2~codex:2',
  kind: 'differ',
  positions: [
    { agents: ['claude'], units: ['claude:2'], numberSpans: [{ unit: 'claude:2', start: 7, end: 10 }] },
    {
      agents: ['codex', 'opencode'],
      units: ['codex:2', 'opencode:2'],
      numberSpans: [
        { unit: 'codex:2', start: 7, end: 10 },
        { unit: 'opencode:2', start: 7, end: 10 },
      ],
    },
  ],
  numbersDiffer: true,
  check: [],
  missing: [],
};

// The snapshot's items come in an order the cards must not follow, with an
// unknown kind, a unit no answer has and a repeated key.
function snapshot(over: Partial<Comparison> = {}): Comparison {
  return {
    comparisonId: 'c1',
    status: 'done',
    source: 'run',
    runId: 'r1',
    engine: 'pc',
    answers: [
      {
        id: 'claude',
        agent: 'claude',
        via: 'pc',
        family: 'anthropic',
        model: 'claude-standard',
        status: 'ready',
        notJudged: 0,
        units: [unit('claude:1', 1, 'Seoul is the capital.'), unit('claude:2', 2, 'It has 9.4 million people.'), unit('claude:3', 3, 'Try the palace.')],
      },
      {
        id: 'codex',
        agent: 'codex',
        via: 'pc',
        family: 'openai',
        status: 'ready',
        notJudged: 0,
        units: [unit('codex:1', 1, 'Seoul is the capital.'), unit('codex:2', 2, 'It has 9.7 million people.')],
      },
      {
        id: 'opencode',
        agent: 'opencode',
        via: 'pc',
        family: 'deepseek',
        status: 'ready',
        notJudged: 0,
        units: [
          unit('opencode:1', 1, 'Seoul is the capital.'),
          unit('opencode:2', 2, 'It has 9.7 million people.'),
          unit('opencode:3', 3, 'Eat at the market.'),
        ],
      },
    ],
    requests: [
      req('r1', 'claude', 'codex', 'opencode'),
      req('r2', 'codex', 'claude', 'opencode'),
      req('r3', 'claude', 'opencode', 'codex'),
      req('r4', 'opencode', 'claude', 'codex'),
      req('r5', 'codex', 'opencode', 'claude'),
      req('r6', 'opencode', 'codex', 'claude'),
    ],
    judgments: [
      judgment('claude:2', 'codex', 'opencode', 'opposite', { evidence: ['codex:2'] }),
      judgment('codex:2', 'claude', 'opencode', 'opposite', { evidence: ['claude:2'] }),
      judgment('claude:1', 'codex', 'opencode'),
    ],
    items: [
      { key: 'o:claude:3', kind: 'onlyOne', positions: [{ agents: ['claude'], units: ['claude:3'] }], check: [], missing: [] },
      {
        key: 'a:claude:1',
        kind: 'agree',
        positions: [{ agents: ['claude', 'codex', 'opencode'], units: ['claude:1', 'codex:1', 'opencode:1'] }],
        check: [],
        missing: [],
      },
      { key: 'x:1', kind: 'brandNew', positions: [{ agents: ['claude'], units: ['claude:1'] }], check: [], missing: [] },
      differItem,
      { key: 'c:claude:9', kind: 'check', positions: [{ agents: ['claude'], units: ['claude:9'] }], check: ['unsure'], missing: [] },
      { key: 'a:claude:1', kind: 'agree', positions: [{ agents: ['codex'], units: ['codex:1'] }], check: [], missing: [] },
      { key: 'o:opencode:3', kind: 'onlyOne', positions: [{ agents: ['opencode'], units: ['opencode:3'] }], check: [], missing: [] },
    ],
    counts: { ...counts, differ: 1, agree: 1, onlyOne: 2 },
    ...over,
  };
}

const running = (over: Partial<Comparison> = {}) => snapshot({ status: 'running', pollAfterMs: 1000, ...over });

// ---- Cards ----

test('cards keep their order whatever the snapshot order, and items keep the snapshot order', () => {
  const cs = cards(snapshot());
  expect(cs.map((c) => c.kind)).toEqual(['differ', 'check', 'agree', 'onlyOne']);
  expect(cs.map((c) => c.items.map((i) => i.key))).toEqual([['d:claude:2~codex:2'], [], ['a:claude:1'], ['o:claude:3', 'o:opencode:3']]);
  const differ = cs[0].items[0];
  expect(differ.sides.map((s) => s.agents)).toEqual([['Claude'], ['Codex', 'OpenCode']]);
  expect(differ.sides[0].units[0]).toEqual({
    id: 'claude:2',
    answer: 'claude',
    agent: 'Claude',
    kind: 'sentence',
    text: 'It has 9.4 million people.',
    math: [],
  });
  expect(differ.sides[0].numberSpans).toEqual([{ unit: 'claude:2', start: 7, end: 10 }]);
  expect(differ.numbersDiffer).toBe(true);
  // A snapshot with nothing in it still has the four cards.
  expect(cards(snapshot({ items: [], answers: [] })).map((c) => c.items.length)).toEqual([0, 0, 0, 0]);
});

test('what cannot be shown is left out: unknown kinds, unknown units, empty sides, repeated keys', () => {
  const s = snapshot({
    items: [
      { key: 'k1', kind: 'agree', positions: [{ agents: ['claude', 'ghost'], units: ['claude:1', 'ghost:1'] }, { agents: ['x'], units: ['nope:1'] }], check: [], missing: ['ghost'] },
      { key: 'k1', kind: 'agree', positions: [{ agents: ['codex'], units: ['codex:1'] }], check: [], missing: [] },
      { key: 'k2', kind: 'onlyOne', positions: [{ agents: ['codex'], units: ['nope:2'] }], check: [], missing: [] },
      { key: 'k3', kind: 'check', positions: [], check: [], missing: [] },
      { key: 'k4', kind: 'tomorrow', positions: [{ agents: ['codex'], units: ['codex:1'] }], check: [], missing: [] },
      // Missing arrays (a sloppy snapshot) are read as empty.
      { key: 'k5', kind: 'onlyOne', positions: [{ agents: ['codex'], units: ['codex:2'] }] } as unknown as CardItem,
    ],
  });
  const cs = cards(s);
  const agree = cs.find((c) => c.kind === 'agree')?.items ?? [];
  expect(agree.map((i) => i.key)).toEqual(['k1']);
  expect(agree[0].sides).toHaveLength(1);
  expect(agree[0].sides[0].units.map((u) => u.id)).toEqual(['claude:1']);
  // An answer id the snapshot does not know shows as it is.
  expect(agree[0].sides[0].agents).toEqual(['Claude', 'ghost']);
  expect(agree[0].missing).toEqual(['ghost']);
  expect(cs.find((c) => c.kind === 'onlyOne')?.items.map((i) => i.key)).toEqual(['k5']);
  expect(cs.find((c) => c.kind === 'check')?.items).toEqual([]);
  expect(cs.flatMap((c) => c.items).find((i) => i.key === 'k4')).toBeUndefined();
});

test('chips count the shown items, and a chip shows one card', () => {
  const cs = cards(snapshot());
  // The snapshot's own counts are not used (the check item there cannot be shown).
  expect(chipCounts(cs)).toEqual({ differ: 1, check: 0, agree: 1, onlyOne: 2 });
  expect(shownCards(cs, { only: 'agree', ended: true }).map((c) => c.kind)).toEqual(['agree']);
  expect(shownCards(cs, { only: 'check', ended: true }).map((c) => c.kind)).toEqual(['check']);
  expect(shownCards(cs, { only: 'onlyOne', ended: false }).map((c) => c.kind)).toEqual(['onlyOne']);
});

test('while judges run only Differ shows; at the end empty cards hide but Differ stays', () => {
  const cs = cards(snapshot());
  expect(shownCards(cs, { only: null, ended: false }).map((c) => c.kind)).toEqual(['differ']);
  expect(shownCards(cs, { only: null, ended: true }).map((c) => c.kind)).toEqual(['differ', 'agree', 'onlyOne']);
  const none = cards(snapshot({ items: [] }));
  expect(shownCards(none, { only: null, ended: true }).map((c) => c.kind)).toEqual(['differ']);
  expect(shownCards(none, { only: null, ended: false }).map((c) => c.kind)).toEqual(['differ']);
});

test('item keys pass through unchanged from a running to a done snapshot', () => {
  const early = running({ items: [differItem], requests: snapshot().requests.map((r, i) => (i < 3 ? r : { ...r, status: 'queued' })) });
  const late = snapshot();
  const keyOf = (s: Comparison) => cards(s)[0].items[0].key;
  expect(keyOf(early)).toBe('d:claude:2~codex:2');
  expect(keyOf(late)).toBe(keyOf(early));
  expect(pairView(early, keyOf(early))?.item.sides[0].units[0].text).toBe(pairView(late, keyOf(early))?.item.sides[0].units[0].text);
});

test('judgments by the author of either side, of an answer against itself or of unknown ids are dropped', () => {
  const keep1 = judgment('claude:2', 'codex', 'opencode', 'opposite');
  const keep2 = judgment('codex:2', 'claude', 'opencode', 'opposite');
  const s = snapshot({
    judgments: [
      keep1,
      judgment('claude:2', 'codex', 'claude'), // wrote the unit
      judgment('claude:2', 'codex', 'codex'), // wrote the answer it was read against
      judgment('claude:2', 'claude', 'opencode'), // the same answer
      judgment('nope:1', 'codex', 'opencode'),
      judgment('claude:2', 'nobody', 'opencode'),
      keep2,
    ],
  });
  expect(usableJudgments(s)).toEqual([keep1, keep2]);
  expect(usableJudgments(snapshot({ judgments: undefined as unknown as Judgment[] }))).toEqual([]);
});

test('the pair sheet: both sides, judgments per direction in names, known checks, the AIs to open', () => {
  const s = snapshot();
  const pv = pairView(s, 'd:claude:2~codex:2');
  expect(pv?.item.kind).toBe('differ');
  expect(pv?.lines).toEqual([
    { judge: 'OpenCode', from: 'Claude', against: 'Codex', unit: 'claude:2', label: 'opposite', unsure: false },
    { judge: 'OpenCode', from: 'Codex', against: 'Claude', unit: 'codex:2', label: 'opposite', unsure: false },
  ]);
  expect(pv?.agents).toEqual(['claude', 'codex', 'opencode']);
  expect(pv?.why).toEqual([]);
  expect(pairView(s, 'no-such-key')).toBeNull();
  expect(pairView(s, 'x:1')).toBeNull(); // an unknown kind is not shown, so it has no sheet

  const checked = snapshot({
    items: [{ key: 'c:claude:2', kind: 'check', positions: [{ agents: ['claude'], units: ['claude:2'] }], check: ['brandNewCheck', 'unsure', 'noEvidence'], missing: [] }],
    judgments: [judgment('claude:2', 'codex', 'opencode', 'tomorrow', { unsure: true })],
  });
  const cv = pairView(checked, 'c:claude:2');
  expect(cv?.why).toEqual(['unsure', 'noEvidence']);
  expect(cv?.lines).toEqual([{ judge: 'OpenCode', from: 'Claude', against: 'Codex', unit: 'claude:2', label: 'tomorrow', unsure: true }]);
  expect(cv?.agents).toEqual(['claude']);
});

test('words for checks and labels; unknown ones are safe', () => {
  expect(whyText('unsure')).toBe('The judging AI was not sure.');
  expect(whyText('numbersAbsent')).toBe('Judged not mentioned, but the other answer has other numbers.');
  expect(whyText('brandNewCheck')).toBeNull();
  expect(labelText('same')).toBe('Same');
  expect(labelText('opposite')).toBe('Differs');
  expect(labelText('absent')).toBe('Not mentioned');
  expect(labelText('tomorrow')).toBe('tomorrow');
});

// ---- State ----

test('compareState: no Summary tab, no judge, still answering', () => {
  expect(compareState(turnOf())).toBeNull();
  expect(compareState(turnOf({ compare: tc({ start: 'noJudgeOff', judges: [] }) }))).toEqual({ kind: 'noJudge', why: 'off' });
  expect(compareState(turnOf({ compare: tc({ start: 'noJudgeNone', judges: [] }), run: runOf('done', answers3) }))).toEqual({
    kind: 'noJudge',
    why: 'none',
  });
  expect(compareState(turnOf({ compare: tc() }))).toEqual({ kind: 'answering' });
  expect(compareState(turnOf({ compare: tc({ id: 'c1' }), run: runOf('running', answers3.slice(0, 1)) }))).toEqual({ kind: 'answering' });
});

test('compareState: errors kept on the turn', () => {
  expect(compareState(doneTurn({ error: 'network', key: 'k' }))).toEqual({ kind: 'lost', action: 'start' });
  expect(compareState(doneTurn({ error: 'network', id: 'c1' }))).toEqual({ kind: 'lost', action: 'poll' });
  expect(compareState(doneTurn({ error: 'comparison_not_found', id: 'c1' }))).toEqual({ kind: 'notFound' });
  expect(compareState(doneTurn({ error: 'need_two_answers' }))).toEqual({ kind: 'oneAnswer' });
  for (const code of ['too_many_comparisons', 'internal', 'bad_compare', 'ask_folder_unavailable']) {
    expect(compareState(doneTurn({ error: code }))).toEqual({ kind: 'failed', code, action: 'start' });
    expect(compareState(doneTurn({ error: code }, answers3.slice(0, 1)))).toEqual({ kind: 'failed', code, action: null });
  }
  expect(compareState(doneTurn({ error: 'no_judge' }))).toEqual({ kind: 'failed', code: 'no_judge', action: null });
  expect(compareState(doneTurn({ error: 'unauthorized' }))).toEqual({ kind: 'failed', code: 'unauthorized', action: null });
});

test('compareState: before the comparison', () => {
  const one = [answers3[0], { agent: 'codex', status: 'failed', error: { code: 'ask_timeout' } }, done('opencode', '   ')];
  expect(compareState(doneTurn({}, one))).toEqual({ kind: 'oneAnswer' });
  expect(compareState(doneTurn({ key: 'k' }))).toEqual({ kind: 'comparing', ended: 0, total: 0 });
  // 다시 비교 after a 404: the new POST is on its way, the old id still set.
  expect(compareState(doneTurn({ key: 'k', id: 'old' }))).toEqual({ kind: 'comparing', ended: 0, total: 0 });
  expect(compareState(doneTurn({ start: 'manual' }))).toEqual({ kind: 'manual', cost: 6 });
  // auto, but the agent made no comparison: the person can still start one.
  expect(compareState(doneTurn())).toEqual({ kind: 'manual', cost: 6 });
  expect(compareState(doneTurn({ id: 'c1' }))).toEqual({ kind: 'comparing', ended: 0, total: 0 });
  expect(compareState(doneTurn({ id: 'c2', snap: snapshot() }))).toEqual({ kind: 'comparing', ended: 0, total: 0 });
});

test('compareState: comparing, with no_judge records left out of the total', () => {
  const rs = [
    req('r1', 'claude', 'codex', 'opencode'),
    req('r2', 'codex', 'claude', 'opencode', { status: 'failed', error: { code: 'judge_timeout' } }),
    req('r3', 'claude', 'opencode', 'codex', { status: 'running' }),
    req('r4', 'opencode', 'claude', 'codex', { status: 'queued' }),
    req('r5', 'codex', 'opencode', '', { status: 'failed', error: { code: 'no_judge' } }),
  ];
  const s = running({ requests: rs });
  expect(progress(s)).toEqual({ ended: 2, total: 4 });
  expect(compareState(doneTurn({ id: 'c1', snap: s }))).toEqual({ kind: 'comparing', ended: 2, total: 4 });
  expect(compareState(doneTurn({ id: 'c1', snap: snapshot({ status: 'waiting', pollAfterMs: 1000, requests: [] }) }))).toEqual({
    kind: 'comparing',
    ended: 0,
    total: 0,
  });
});

test('compareState: an unknown status polls while the PC asks to, and is final otherwise', () => {
  const later = snapshot({ status: 'paused', pollAfterMs: 500 });
  expect(polling(later)).toBe(true);
  expect(compareState(doneTurn({ id: 'c1', snap: later }))).toEqual({ kind: 'comparing', ended: 6, total: 6 });
  const archived = snapshot({ status: 'archived' });
  expect(isFinal(archived)).toBe(true);
  expect(compareState(doneTurn({ id: 'c1', snap: archived }))).toEqual({ kind: 'done', partial: false, retry: false });
});

test('compareState: done, partial and failed', () => {
  expect(compareState(doneTurn({ id: 'c1', snap: snapshot() }))).toEqual({ kind: 'done', partial: false, retry: false });
  const someFailed = snapshot({
    requests: [
      ...snapshot().requests.slice(0, 4),
      req('r5', 'codex', 'opencode', 'claude', { status: 'failed', error: { code: 'judge_failed', message: 'usage limit reached' } }),
      req('r6', 'opencode', 'codex', 'claude', { status: 'failed', error: { code: 'judge_failed', message: 'usage limit reached' } }),
    ],
  });
  expect(compareState(doneTurn({ id: 'c1', snap: someFailed }))).toEqual({ kind: 'done', partial: true, retry: true });
  // A pair no AI could judge is not a failed judgment.
  const noJudgeRecord = snapshot({ requests: [...snapshot().requests, req('r7', 'codex', 'opencode', '', { status: 'failed', error: { code: 'no_judge' } })] });
  expect(compareState(doneTurn({ id: 'c1', snap: noJudgeRecord }))).toEqual({ kind: 'done', partial: false, retry: false });

  const allFailed = snapshot({
    status: 'failed',
    error: { code: 'judge_failed', message: 'usage limit reached' },
    requests: snapshot().requests.map((r) => ({ ...r, status: 'failed', error: { code: 'judge_failed', message: 'usage limit reached' } })),
    items: [],
  });
  expect(compareState(doneTurn({ id: 'c1', snap: allFailed }))).toEqual({
    kind: 'failed',
    code: 'judge_failed',
    message: 'usage limit reached',
    action: 'retry',
  });
  const two = snapshot({ status: 'failed', error: { code: 'need_two_answers' }, requests: [] });
  expect(compareState(doneTurn({ id: 'c1', snap: two }))).toEqual({ kind: 'oneAnswer' });
  const noJudge = snapshot({ status: 'failed', error: { code: 'no_judge' }, requests: [req('r1', 'claude', 'codex', '', { status: 'failed' })] });
  expect(compareState(doneTurn({ id: 'c1', snap: noJudge }))).toEqual({ kind: 'failed', code: 'no_judge', message: undefined, action: null });
  const bare = snapshot({ status: 'failed', requests: [] });
  expect(compareState(doneTurn({ id: 'c1', snap: bare }))).toEqual({ kind: 'failed', code: 'internal', message: undefined, action: 'retry' });
});

test('canRetry: only final comparisons with failed judgments or a failure a retry may fix', () => {
  expect(canRetry(undefined)).toBe(false);
  expect(canRetry(snapshot())).toBe(false);
  const failedOne = snapshot({ requests: [req('r1', 'claude', 'codex', 'opencode', { status: 'failed' })] });
  expect(canRetry(failedOne)).toBe(true);
  expect(canRetry({ ...failedOne, status: 'running', pollAfterMs: 1000 })).toBe(false);
  expect(canRetry(snapshot({ requests: [req('r1', 'claude', 'codex', '', { status: 'failed' })] }))).toBe(false);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'cancelled' }, requests: [] }))).toBe(true);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'no_judge' }, requests: [] }))).toBe(false);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'need_two_answers' }, requests: [] }))).toBe(false);
});

test('compareErrorText looks in compare, then ask, then general words, then the code', () => {
  expect(compareErrorText('judge_timeout')).toBe('The judging did not finish in time.');
  expect(compareErrorText('judge_timeout', 'ignored')).toBe('The judging did not finish in time.');
  expect(compareErrorText('ask_folder_unavailable')).toBe('The PC could not make its ask folder.');
  expect(compareErrorText('workspace_not_trusted', 'C:/ask')).toContain('C:/ask');
  expect(compareErrorText('unauthorized')).toBe('This phone is no longer paired with the PC. Pair again.');
  expect(compareErrorText('network')).toContain('Tailscale');
  expect(compareErrorText('brand_new')).toBe('Something went wrong (brand_new).');
  expect(compareErrorText('judge_failed', 'usage limit reached')).toBe('The judging AI reported an error.\nusage limit reached');
  expect(compareErrorText('judge_failed')).toBe('The judging AI reported an error.');
  // Codes with words in both compare and ask take the compare words (S5 review, R-34).
  expect(compareErrorText('internal')).toBe('Something went wrong on the PC while comparing.');
  expect(compareErrorText('bad_model')).toBe("The judge model could not be used: unknown, or it answered with another company's model.");
  expect(compareErrorText('agent_unavailable')).toBe('That judging AI can no longer judge on this PC.');
});

// ---- Who judges, and what goes ----

test('judgesOf: the asked AIs that can judge, in asked order', () => {
  const info = infoOf([ai('claude', 'anthropic'), ai('codex', 'openai', ['ask']), ai('opencode', 'deepseek', ['ask', 'judge'], false), ai('gemini', 'google')]);
  expect(judgesOf(info, ['gemini', 'codex', 'claude', 'opencode', 'nobody'])).toEqual(['gemini', 'claude']);
  expect(judgesOf(three, ['opencode', 'claude'])).toEqual(['opencode', 'claude']);
});

test('compareStart', () => {
  expect(compareStart({ info: three, asked: all, to: 'all', auto: true })).toBe('auto');
  expect(compareStart({ info: three, asked: all, to: 'all', auto: false })).toBe('manual');
  // Two asked while a third AI that could judge is turned off.
  expect(compareStart({ info: three, asked: ['claude', 'codex'], to: 'all', auto: true })).toBe('noJudgeOff');
  // Two asked and no other AI to judge.
  const twoOnly = infoOf([ai('claude', 'anthropic'), ai('codex', 'openai')]);
  expect(compareStart({ info: twoOnly, asked: ['claude', 'codex'], to: 'all', auto: true })).toBe('noJudgeNone');
  const thirdDown = infoOf([ai('claude', 'anthropic'), ai('codex', 'openai'), ai('opencode', 'deepseek', ['ask', 'judge'], false)]);
  expect(compareStart({ info: thirdDown, asked: ['claude', 'codex'], to: 'all', auto: true })).toBe('noJudgeNone');
  const judgeOnly = infoOf([ai('claude', 'anthropic'), ai('codex', 'openai'), ai('opencode', 'deepseek', ['judge'])]);
  expect(compareStart({ info: judgeOnly, asked: ['claude', 'codex'], to: 'all', auto: true })).toBe('noJudgeNone');
  expect(compareStart({ info: three, asked: ['claude'], to: 'all', auto: true })).toBeNull();
  expect(compareStart({ info: three, asked: ['codex'], to: { agent: 'codex' }, auto: true })).toBeNull();
  // An older agent: no AI judges, so the turn stays as in P2-2.
  const old = infoOf([ai('claude', 'anthropic', ['ask']), ai('codex', 'openai', ['ask']), ai('opencode', 'deepseek', ['ask'])]);
  expect(compareStart({ info: old, asked: all, to: 'all', auto: true })).toBeNull();
  expect(compareStart({ info: old, asked: ['claude', 'codex'], to: 'all', auto: true })).toBeNull();
  // Three asked, none of them judges (a fourth AI would).
  const elsewhere = infoOf([ai('claude', 'anthropic', ['ask']), ai('codex', 'openai', ['ask']), ai('opencode', 'deepseek', ['ask']), ai('gemini', 'google')]);
  expect(compareStart({ info: elsewhere, asked: all, to: 'all', auto: true })).toBeNull();
});

test('earlierFor: questions every judge answered, the latest three, oldest first', () => {
  const t = (question: string, parts: RunPart[]) => turnOf({ question, run: runOf('done', parts) });
  const turns = [
    t('first', answers3),
    t('second', [done('codex', 'only codex')]), // 이 AI에게만
    t('third', [answers3[0], answers3[1], { agent: 'opencode', status: 'failed' }]),
    t('fourth', answers3),
    t('fifth', answers3),
    t('sixth', answers3),
    turnOf({ question: 'lost' }), // never reached the PC
  ];
  expect(earlierFor(turns.slice(0, 3), all)).toEqual(['first']);
  expect(earlierFor(turns, all)).toEqual(['fourth', 'fifth', 'sixth']);
  expect(earlierFor(turns.slice(0, 4), ['claude', 'codex'])).toEqual(['first', 'third', 'fourth']);
  expect(earlierFor(turns, [])).toEqual([]);
  expect(earlierFor([], all)).toEqual([]);
});

test('earlierFor cuts each question to 4 KiB of UTF-8 on a character boundary', () => {
  const long = '가'.repeat(1365) + '😀' + '가'.repeat(2000);
  const [cut] = earlierFor([turnOf({ question: long, run: runOf('done', answers3) })], all);
  expect(cut).toBe('가'.repeat(1365));
  expect(utf8Bytes(cut)).toBe(4095);
  const exact = 'a'.repeat(4096);
  expect(earlierFor([turnOf({ question: exact, run: runOf('done', answers3) })], all)).toEqual([exact]);
  expect(earlierFor([turnOf({ question: exact + 'b', run: runOf('done', answers3) })], all)).toEqual([exact]);
  // NUL is refused by the agent; it is left out.
  expect(earlierFor([turnOf({ question: 'a\u0000b', run: runOf('done', answers3) })], all)).toEqual(['ab']);
});

test('utf8Bytes and cutUtf8 count like UTF-8, a lone surrogate as U+FFFD', () => {
  expect(utf8Bytes('')).toBe(0);
  expect(utf8Bytes('a')).toBe(1);
  expect(utf8Bytes('é')).toBe(2);
  expect(utf8Bytes('가')).toBe(3);
  expect(utf8Bytes('😀')).toBe(4);
  expect(utf8Bytes('\ud800')).toBe(3);
  expect(utf8Bytes('\udc00a')).toBe(4);
  const enc = new TextEncoder();
  for (const s of ['Seoul 서울 😀 é ß', 'a\ud800b', '\ud83d', '😀\ude00', 'x'.repeat(100)]) expect(utf8Bytes(s)).toBe(enc.encode(s).length);
  expect(cutUtf8('😀😀', 5)).toBe('😀');
  expect(cutUtf8('😀😀', 8)).toBe('😀😀');
  expect(cutUtf8('😀', 3)).toBe('');
  expect(cutUtf8('ab', 0)).toBe('');
  expect(cutUtf8('a\ud800', 3)).toBe('a');
  expect(cutUtf8('a\ud800', 4)).toBe('a\ud800');
  expect(cutUtf8('가나다', 8)).toBe('가나');
  expect(cutUtf8('short', 4096)).toBe('short');
});

test('directRequest: the answers that came, with models, judges and earlier questions fixed at send time', () => {
  const parts: RunPart[] = [
    done('opencode', 'C'),
    done('claude', 'A'),
    done('codex', 'B', null),
    { agent: 'gemini', status: 'failed' },
  ];
  const turn = turnOf({ agents: ['claude', 'codex', 'opencode', 'gemini'], run: runOf('done', parts), compare: tc({ start: 'manual', earlier: ['first'], judges: ['claude', 'codex'] }) });
  expect(directRequest(turn, 'k1')).toEqual({
    clientComparisonId: 'k1',
    question: 'Capital?',
    earlier: ['first'],
    judges: ['claude', 'codex'],
    answers: [
      { id: 'claude', agent: 'claude', via: 'pc', model: 'claude-standard', text: 'A' },
      { id: 'codex', agent: 'codex', via: 'pc', text: 'B' },
      { id: 'opencode', agent: 'opencode', via: 'pc', model: 'opencode-standard', text: 'C' },
    ],
  });
  const blank = turnOf({ run: runOf('done', [done('claude', 'A'), done('codex', '  \n'), { agent: 'opencode', status: 'running', answer: 'half' }]), compare: tc() });
  expect(directRequest(blank, 'k2')).toBeNull();
  expect(directRequest(turnOf({ compare: tc() }), 'k3')).toBeNull();
});

test('directRequest cuts a long answer to 64 KiB and a long question to 32 KiB, on character boundaries', () => {
  const hangul = '가'.repeat(24000); // 72,000 bytes
  const emoji = '😀'.repeat(17000); // 68,000 bytes
  const turn = turnOf({ question: '질'.repeat(12000), run: runOf('done', [done('claude', hangul), done('codex', emoji), done('opencode', 'short')]), compare: tc() });
  const r = directRequest(turn, 'k');
  const [a, b] = r?.answers ?? [];
  expect(utf8Bytes(a.text)).toBeLessThanOrEqual(65536);
  expect(hangul.startsWith(a.text)).toBe(true);
  expect(a.text.length).toBe(21845);
  expect(utf8Bytes(b.text)).toBe(65536);
  expect(emoji.startsWith(b.text)).toBe(true);
  expect(b.text.length % 2).toBe(0);
  expect(utf8Bytes(r?.question ?? '')).toBeLessThanOrEqual(32768);
  expect(r?.question).toBe('질'.repeat(10922));
});

test('startCost: every ordered pair once; two answers need a judge that wrote none', () => {
  expect(startCost(doneTurn({ start: 'manual' }))).toBe(6);
  const four = turnOf({
    agents: ['claude', 'codex', 'opencode', 'gemini'],
    run: runOf('done', [...answers3, done('gemini', 'G')]),
    compare: tc({ start: 'manual', judges: ['claude', 'codex', 'opencode', 'gemini'] }),
  });
  expect(startCost(four)).toBe(12);
  const twoAndJudge = doneTurn({ start: 'manual' }, [answers3[0], answers3[1], { agent: 'opencode', status: 'failed' }]);
  expect(startCost(twoAndJudge)).toBe(2);
  const twoNoJudge = doneTurn({ start: 'manual', judges: ['claude', 'codex'] }, [answers3[0], answers3[1], { agent: 'opencode', status: 'failed' }]);
  expect(startCost(twoNoJudge)).toBe(0);
  // Nothing to start: no Compare button, the no_judge state instead.
  expect(compareState(twoNoJudge)).toEqual({ kind: 'failed', code: 'no_judge', action: null });
  expect(startCost(doneTurn({ start: 'manual' }, [answers3[0]]))).toBe(0);
});

// ---- State updates ----

test('withRun takes the run comparison once and never clears', () => {
  expect(withRun(undefined, runOf('done', [], { comparisonId: 'c1' }))).toBeUndefined();
  const c = tc();
  expect(withRun(c, runOf('running', []))).toBe(c);
  expect(withRun(c, runOf('running', [], { comparisonId: 'c1' }))).toEqual({ ...c, id: 'c1' });
  // A direct comparison made after a 404 keeps its own id.
  const direct = tc({ id: 'c9' });
  expect(withRun(direct, runOf('done', [], { comparisonId: 'c1' }))).toBe(direct);
  expect(withRun(c, runOf('running', [], { compareError: { code: 'too_many_comparisons', message: 'busy' } }))).toEqual({
    ...c,
    error: 'too_many_comparisons',
  });
  const failed = tc({ error: 'network' });
  expect(withRun(failed, runOf('running', [], { compareError: { code: 'bad_compare' } }))).toBe(failed);
  expect(withRun(failed, runOf('running', []))).toBe(failed);
  expect(withRun(direct, runOf('running', [], { compareError: { code: 'bad_compare' } }))).toBe(direct);
});

test('withSnap and withError', () => {
  const s = snapshot();
  expect(withSnap(tc({ error: 'network', key: 'k' }), s)).toEqual({ ...tc(), key: 'k', id: 'c1', snap: s, error: undefined });
  const c = tc({ id: 'c1' });
  expect(withError(c, new CancelledError())).toBe(c);
  expect(withError(c, new AgentError('comparison_not_found', 404))).toEqual({ ...c, error: 'comparison_not_found' });
  expect(withError(c, new NetworkError())).toEqual({ ...c, error: 'network' });
  expect(withError(c, new Error('boom'))).toEqual({ ...c, error: 'internal' });
  expect(errorCode(new AgentError('http_502', 502))).toBe('http_502');
});

// ---- Drawing a unit ----

test('pieces never look for "$" themselves', () => {
  expect(pieces('$HOME/bin:$PATH', [], [])).toEqual([{ text: '$HOME/bin:$PATH' }]);
  expect(pieces('$HOME/bin:$PATH', undefined, [])).toEqual([{ text: '$HOME/bin:$PATH' }]);
  expect(pieces('', [], [])).toEqual([]);
});

test('pieces draw math at the given spans, bold at the number spans', () => {
  const text = 'Area is $\\pi r^2$ here';
  expect(pieces(text, [{ start: 8, end: 17, display: false }], [])).toEqual([
    { text: 'Area is ' },
    { text: '$\\pi r^2$', math: { tex: '\\pi r^2', display: false } },
    { text: ' here' },
  ]);
  expect(pieces('So $$x^2$$.', [{ start: 3, end: 10, display: true }], [])[1]).toEqual({ text: '$$x^2$$', math: { tex: 'x^2', display: true } });
  expect(pieces('It has 9.4 million people.', [], [{ start: 7, end: 10 }])).toEqual([
    { text: 'It has ' },
    { text: '9.4', bold: true },
    { text: ' million people.' },
  ]);
  // Bold over math marks the math piece.
  expect(pieces('$9x$ y', [{ start: 0, end: 4, display: false }], [{ start: 1, end: 2 }])).toEqual([
    { text: '$9x$', math: { tex: '9x', display: false }, bold: true },
    { text: ' y' },
  ]);
  // A span without math marks stays text.
  expect(pieces('abc def', [{ start: 0, end: 3, display: false }], [])).toEqual([{ text: 'abc' }, { text: ' def' }]);
});

test('pieces clamp bold, drop bad or overlapping math, and merge bold', () => {
  const text = 'It has 9.4 million people.'; // 26
  expect(pieces(text, [], [{ start: -5, end: 2 }])).toEqual([{ text: 'It', bold: true }, { text: text.slice(2) }]);
  expect(pieces(text, [], [{ start: 20, end: 100 }])).toEqual([{ text: text.slice(0, 20) }, { text: text.slice(20), bold: true }]);
  expect(pieces(text, [], [{ start: 1.5, end: 3 }, { start: 5, end: 5 }, { start: 9, end: 4 }])).toEqual([{ text }]);
  // Overlapping and touching bold spans become one.
  expect(pieces(text, [], [{ start: 2, end: 5 }, { start: 0, end: 3 }, { start: 5, end: 6 }])).toEqual([{ text: 'It has', bold: true }, { text: text.slice(6) }]);
  const m = '$a$ $b$';
  expect(pieces(m, [{ start: 0, end: 3, display: false }, { start: 1, end: 7, display: false }], [])).toEqual([
    { text: '$a$', math: { tex: 'a', display: false } },
    { text: ' $b$' },
  ]);
  expect(pieces(m, [{ start: 4, end: 9, display: false }, { start: 0, end: 0, display: false }, { start: 0.5, end: 3, display: false }], [])).toEqual([{ text: m }]);
  // Out of order spans are read in order.
  expect(pieces(m, [{ start: 4, end: 7, display: false }, { start: 0, end: 3, display: false }], []).filter((p) => p.math)).toHaveLength(2);
});

test('pieces keep emoji whole (UTF-16 offsets)', () => {
  const text = '😀 $x$ 😀'; // emoji are two code units each
  expect(pieces(text, [{ start: 3, end: 6, display: false }], [])).toEqual([
    { text: '😀 ' },
    { text: '$x$', math: { tex: 'x', display: false } },
    { text: ' 😀' },
  ]);
  expect(pieces(text, [], [{ start: 1, end: 2 }])).toEqual([{ text: '😀', bold: true }, { text: ' $x$ 😀' }]);
  expect(pieces(text, [], [{ start: 8, end: 9 }])).toEqual([{ text: '😀 $x$ ' }, { text: '😀', bold: true }]);
  expect(pieces(text, [], [{ start: 0, end: 1 }])).toEqual([{ text: '😀', bold: true }, { text: ' $x$ 😀' }]);
});

// ---- Tabs and lines ----

test('tabs: Summary first, then each asked AI with how its answer stands', () => {
  expect(tabs(turnOf())).toBeNull();
  const t = turnOf({
    compare: tc(),
    run: runOf('running', [done('claude', 'A'), { agent: 'codex', status: 'running' }, { agent: 'gemini', status: 'done' }]),
  });
  expect(tabs(t)).toEqual([
    { key: 'summary', mark: '' },
    { key: 'claude', agent: 'claude', mark: '' },
    { key: 'codex', agent: 'codex', mark: '…' },
    { key: 'opencode', agent: 'opencode', mark: '…' },
  ]);
  const ended = turnOf({ compare: tc(), run: runOf('done', [done('claude', 'A'), { agent: 'codex', status: 'failed' }, { agent: 'opencode', status: 'skipped' }]) });
  expect(tabs(ended)?.map((x) => x.mark)).toEqual(['', '', '✕', '✕']);
  const lost = turnOf({ compare: tc(), error: "Can't reach the PC" });
  expect(tabs(lost)?.map((x) => x.mark)).toEqual(['', '✕', '✕', '✕']);
});

test('answered and answeringLines', () => {
  const t = turnOf({ compare: tc(), at: 1000, run: runOf('running', [done('claude', 'A'), { agent: 'codex', status: 'running' }]) });
  expect(answered(t)).toEqual({ n: 1, pc: 1 });
  expect(answered(doneTurn())).toEqual({ n: 3, pc: 3 });
  expect(answeringLines(t, 13500)).toEqual([
    { agent: 'Codex', s: 12 },
    { agent: 'OpenCode', s: 12 },
  ]);
  expect(answeringLines({ ...t, at: undefined }, 13500).map((x) => x.s)).toEqual([0, 0]);
  expect(answeringLines(t, 500).map((x) => x.s)).toEqual([0, 0]);
  expect(answeringLines({ ...t, error: 'lost' }, 13500)).toEqual([]);
  expect(answeringLines(doneTurn(), 13500)).toEqual([]);
});

// ---- The judges ----

test('requestLines, judgedBy, failedWith and longAnswers in names', () => {
  const s = snapshot({
    requests: [
      req('r1', 'claude', 'codex', 'opencode', { ms: 4200 }),
      req('r2', 'codex', 'claude', 'opencode', { status: 'failed', error: { code: 'judge_failed', message: 'usage limit reached' } }),
      req('r3', 'opencode', 'codex', '', { status: 'failed', error: { code: 'no_judge' }, model: undefined }),
      req('r4', 'claude', 'opencode', 'codex', { judgeOnly: true, status: 'running' }),
    ],
  });
  expect(requestLines(s)).toEqual([
    { id: 'r1', judge: 'OpenCode', from: 'Claude', against: 'Codex', judgeOnly: false, model: 'opencode-quick', status: 'done', ms: 4200 },
    {
      id: 'r2',
      judge: 'OpenCode',
      from: 'Codex',
      against: 'Claude',
      judgeOnly: false,
      model: 'opencode-quick',
      status: 'failed',
      error: { code: 'judge_failed', message: 'usage limit reached' },
    },
    { id: 'r3', judge: '', from: 'OpenCode', against: 'Codex', judgeOnly: false, status: 'failed', error: { code: 'no_judge' } },
    { id: 'r4', judge: 'Codex', from: 'Claude', against: 'OpenCode', judgeOnly: true, model: 'codex-quick', status: 'running' },
  ]);
  // In the snapshot's answer order, not the requests' order.
  expect(judgedBy(s, 'claude')).toEqual(['Codex', 'OpenCode']);
  // Codex's judging failed: only OpenCode judged Codex's answer.
  expect(judgedBy(s, 'codex')).toEqual([]);
  expect(judgedBy(s, 'opencode')).toEqual([]);
  expect(judgedBy(undefined, 'claude')).toEqual([]);
  // Only pairs with a judge count as failed, in answer order.
  expect(failedWith(s)).toEqual(['Claude', 'Codex']);
  expect(failedWith(snapshot())).toEqual([]);
  expect(longAnswers(s)).toEqual([]);
  const long = snapshot();
  long.answers[0] = { ...long.answers[0], notJudged: 7, units: long.answers[0].units.map((u, i) => ({ ...u, judged: i < 2 })) };
  expect(longAnswers(long)).toEqual([{ agent: 'Claude', judged: 2 }]);
});

test('judgeModels: from the snapshot, before it from the turn and the PC', () => {
  const s = snapshot();
  // In the turn's order (as the tabs), not the order the requests came in.
  expect(judgeModels(doneTurn({ id: 'c1', snap: s }), three)).toEqual([
    { agent: 'Claude', model: 'claude-quick' },
    { agent: 'Codex', model: 'codex-quick' },
    { agent: 'OpenCode', model: 'opencode-quick' },
  ]);
  // A judge the turn did not ask comes last.
  expect(judgeModels({ ...doneTurn({ id: 'c1', snap: s }), agents: ['codex', 'opencode'] }, three)).toEqual([
    { agent: 'Codex', model: 'codex-quick' },
    { agent: 'OpenCode', model: 'opencode-quick' },
    { agent: 'Claude', model: 'claude-quick' },
  ]);
  expect(judgeModels(doneTurn({ judges: ['claude', 'codex'] }), three)).toEqual([
    { agent: 'Claude', model: 'claude-quick' },
    { agent: 'Codex', model: 'codex-quick' },
  ]);
  expect(judgeModels(doneTurn({ judges: ['claude'] }), null)).toEqual([{ agent: 'Claude' }]);
  // A snapshot of another comparison (a lost one) does not count.
  expect(judgeModels(doneTurn({ id: 'c2', snap: s, judges: ['codex'] }), three)).toEqual([{ agent: 'Codex', model: 'codex-quick' }]);
  expect(judgeModels(turnOf(), three)).toEqual([]);
});
