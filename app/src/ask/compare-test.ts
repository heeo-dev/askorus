import { AgentError, CancelledError, NetworkError } from '@/api/client';
import type { AdapterInfo, Comparison, Info, Run, RunPart, Synthesis } from '@/api/types';
import { setLang } from '@/i18n';

import {
  answered,
  answeringLines,
  canRetry,
  canSynthesize,
  compareErrorText,
  compares,
  compareState,
  comparing,
  cutUtf8,
  directRequest,
  earlierFor,
  errorCode,
  isFinal,
  polling,
  synthBy,
  synthErrorText,
  synthRequest,
  synthState,
  tabs,
  utf8Bytes,
  withError,
  withRun,
  withSnap,
  writerFor,
  writersOf,
} from './compare';
import type { Turn, TurnCompare } from './thread';

// ---- Fixtures ----

const ai = (agent: string, caps = ['ask', 'synthesize'], available = true): AdapterInfo => ({ agent, available, capabilities: caps });

const infoOf = (agents: AdapterInfo[]): Info => ({
  name: 'askorus-agent',
  version: '0.3.0',
  apiVersion: 1,
  hostname: 'pc',
  os: 'windows',
  agents,
  capabilities: ['ask'],
  reserved: [],
});

const three = infoOf([ai('claude'), ai('codex'), ai('opencode')]);
// An S5 agent or older: its AIs answer but do not write.
const askOnly = infoOf([ai('claude', ['ask']), ai('codex', ['ask']), ai('opencode', ['ask'])]);
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

// A snapshot as the agent sends it since C6: done at once, no judging.
function snapshot(over: Partial<Comparison> = {}): Comparison {
  return { comparisonId: 'c1', status: 'done', source: 'run', runId: 'r1', engine: 'pc', ...over };
}

// ---- State ----

test('compareState: no comparison, one kept from before C6 that had none, still answering', () => {
  expect(compareState(turnOf())).toBeNull();
  // Two AIs before C6: no comparison, each answer in full.
  expect(compareState(turnOf({ compare: tc({ start: 'noJudgeOff', judges: [] }) }))).toBeNull();
  expect(compareState(turnOf({ compare: tc({ start: 'noJudgeNone', judges: [] }), run: runOf('done', answers3) }))).toBeNull();
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
  expect(compareState(doneTurn({ key: 'k' }))).toEqual({ kind: 'comparing' });
  // 다시 비교 after a 404: the new POST is on its way, the old id still set.
  expect(compareState(doneTurn({ key: 'k', id: 'old' }))).toEqual({ kind: 'comparing' });
  // Compare by hand, kept from before C6, or a PC that made none: 비교하기 makes one.
  expect(compareState(doneTurn({ start: 'manual' }))).toEqual({ kind: 'manual' });
  expect(compareState(doneTurn())).toEqual({ kind: 'manual' });
  expect(compareState(doneTurn({ id: 'c1' }))).toEqual({ kind: 'comparing' });
  expect(compareState(doneTurn({ id: 'c2', snap: snapshot() }))).toEqual({ kind: 'comparing' });
});

test('compareState: comparing while the PC makes it, then done or failed', () => {
  expect(compareState(doneTurn({ id: 'c1', snap: snapshot({ status: 'waiting', pollAfterMs: 1000 }) }))).toEqual({ kind: 'comparing' });
  expect(compareState(doneTurn({ id: 'c1', snap: snapshot() }))).toEqual({ kind: 'done' });
  // A turn kept from before C6 that was compared by hand shows the same way.
  expect(compareState(doneTurn({ start: 'manual', id: 'c1', snap: snapshot() }))).toEqual({ kind: 'done' });
  const stopped = snapshot({ status: 'failed', error: { code: 'cancelled', message: 'cancelled' } });
  expect(compareState(doneTurn({ id: 'c1', snap: stopped }))).toEqual({ kind: 'failed', code: 'cancelled', message: 'cancelled', action: 'retry' });
  const two = snapshot({ status: 'failed', error: { code: 'need_two_answers' } });
  expect(compareState(doneTurn({ id: 'c1', snap: two }))).toEqual({ kind: 'oneAnswer' });
  const noJudge = snapshot({ status: 'failed', error: { code: 'no_judge' } });
  expect(compareState(doneTurn({ id: 'c1', snap: noJudge }))).toEqual({ kind: 'failed', code: 'no_judge', message: undefined, action: null });
  const bare = snapshot({ status: 'failed' });
  expect(compareState(doneTurn({ id: 'c1', snap: bare }))).toEqual({ kind: 'failed', code: 'internal', message: undefined, action: 'retry' });
});

test('compareState: an unknown status polls while the PC asks to, and is final otherwise', () => {
  const later = snapshot({ status: 'paused', pollAfterMs: 500 });
  expect(polling(later)).toBe(true);
  expect(compareState(doneTurn({ id: 'c1', snap: later }))).toEqual({ kind: 'comparing' });
  const archived = snapshot({ status: 'archived' });
  expect(isFinal(archived)).toBe(true);
  expect(compareState(doneTurn({ id: 'c1', snap: archived }))).toEqual({ kind: 'done' });
});

test('canRetry: only a final comparison that failed for a reason a retry may fix', () => {
  expect(canRetry(undefined)).toBe(false);
  expect(canRetry(snapshot())).toBe(false);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'cancelled' } }))).toBe(true);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'cancelled' }, pollAfterMs: 1000 }))).toBe(false);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'no_judge' } }))).toBe(false);
  expect(canRetry(snapshot({ status: 'failed', error: { code: 'need_two_answers' } }))).toBe(false);
});

test('compareErrorText looks in compare, then ask, then general words, then the code', () => {
  expect(compareErrorText('cancelled')).toBe('The comparison was stopped.');
  expect(compareErrorText('cancelled', 'ignored')).toBe('The comparison was stopped.');
  expect(compareErrorText('ask_folder_unavailable')).toBe('The PC could not make its ask folder.');
  expect(compareErrorText('workspace_not_trusted', 'C:/ask')).toContain('C:/ask');
  expect(compareErrorText('unauthorized')).toBe('This phone is no longer paired with the PC. Pair again.');
  expect(compareErrorText('network')).toContain("Can't reach the PC");
  expect(compareErrorText('brand_new')).toBe('Something went wrong (brand_new).');
  // A judging error kept from before C6 still reads, with the AI's words.
  expect(compareErrorText('judge_failed', 'usage limit reached')).toBe('The judging AI reported an error.\nusage limit reached');
  expect(compareErrorText('judge_failed')).toBe('The judging AI reported an error.');
  // Codes with words in both compare and ask take the compare words (S5 review, R-34).
  expect(compareErrorText('internal')).toBe('Something went wrong on the PC while comparing.');
});

// ---- Who writes, and what goes ----

test('writersOf: the asked AIs that can write, in asked order', () => {
  const info = infoOf([ai('claude'), ai('codex', ['ask']), ai('opencode', ['ask', 'synthesize'], false), ai('gemini')]);
  expect(writersOf(info, ['gemini', 'codex', 'claude', 'opencode', 'nobody'])).toEqual(['gemini', 'claude']);
  expect(writersOf(three, ['opencode', 'claude'])).toEqual(['opencode', 'claude']);
  expect(writersOf(askOnly, all)).toEqual([]);
});

test('compares: two or more AIs asked together on a PC whose AIs can write', () => {
  expect(compares({ info: three, asked: all, to: 'all' })).toBe(true);
  // Two AIs are compared too (C6, #19).
  expect(compares({ info: three, asked: ['claude', 'codex'], to: 'all' })).toBe(true);
  // Any AI of the PC that writes will do: the PC picks the writer among the asked.
  expect(compares({ info: infoOf([ai('claude', ['ask']), ai('codex', ['ask']), ai('opencode')]), asked: ['claude', 'codex'], to: 'all' })).toBe(true);
  expect(compares({ info: three, asked: ['claude'], to: 'all' })).toBe(false);
  expect(compares({ info: three, asked: ['codex'], to: { agent: 'codex' } })).toBe(false);
  // An older agent: no AI writes, so the turn stays as in P2-2.
  expect(compares({ info: askOnly, asked: all, to: 'all' })).toBe(false);
  expect(compares({ info: infoOf([ai('claude', ['ask', 'synthesize'], false), ai('codex', ['ask'])]), asked: ['claude', 'codex'], to: 'all' })).toBe(false);
});

test('earlierFor: questions every writer answered, the latest three, oldest first', () => {
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

test('directRequest: the answers that came, with models, writers and earlier questions fixed at send time', () => {
  const parts: RunPart[] = [
    done('opencode', 'C'),
    done('claude', 'A'),
    done('codex', 'B', null),
    { agent: 'gemini', status: 'failed' },
  ];
  const turn = turnOf({ agents: ['claude', 'codex', 'opencode', 'gemini'], run: runOf('done', parts), compare: tc({ earlier: ['first'], judges: ['claude', 'codex'] }) });
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

test('directRequest carries the combined answer fixed at send time, with the tier', () => {
  const withTier = (synth: TurnCompare['synth'], tier?: 'quick' | 'deep') => {
    const t = doneTurn({ synth });
    return { ...t, req: tier ? { ...t.req, tier } : t.req };
  };
  expect(directRequest(withTier({ agent: 'codex' }, 'deep'), 'k')?.synthesis).toEqual({ agent: 'codex', tier: 'deep' });
  expect(directRequest(withTier({ off: true }, 'quick'), 'k')?.synthesis).toEqual({ off: true, tier: 'quick' });
  expect(directRequest(withTier({}, 'quick'), 'k')?.synthesis).toEqual({ tier: 'quick' });
  expect(directRequest(withTier({ agent: 'claude' }), 'k')?.synthesis).toEqual({ agent: 'claude' });
  // A turn kept from an S5 PC (no synth): the S5 body.
  expect('synthesis' in (directRequest(withTier(undefined, 'deep'), 'k') ?? {})).toBe(false);
  // The turn's synth is not changed by the request.
  const t = withTier({ agent: 'codex' }, 'deep');
  directRequest(t, 'k');
  expect(t.compare?.synth).toEqual({ agent: 'codex' });
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

// ---- The combined answer (S5b) ----

const syn = (over: Partial<Synthesis> = {}): Synthesis => ({ status: 'done', ...over });
const doneSyn: Synthesis = syn({ agent: 'claude', model: 'sonnet', ms: 15230, text: '# Seoul\n\nThe AIs differ.' });
// A comparison with its synthesis; the PC keeps pollAfterMs while the synthesis waits or runs.
const withSyn = (y: Synthesis, over: Partial<Comparison> = {}) =>
  snapshot({ ...(y.status === 'waiting' || y.status === 'running' ? { pollAfterMs: 1000 } : {}), ...over, synthesis: y });
const synTurn = (s: Comparison, c: Partial<TurnCompare> = {}) => doneTurn({ id: s.comparisonId, snap: s, synth: { agent: 'claude' }, ...c });

test('canSynthesize, writerFor and synthRequest', () => {
  expect(canSynthesize(three)).toBe(true);
  expect(canSynthesize(askOnly)).toBe(false); // an S5 agent or older
  expect(canSynthesize(null)).toBe(false);
  expect(canSynthesize(infoOf([ai('claude', ['ask', 'synthesize'], false), ai('codex', ['ask'])]))).toBe(false);
  expect(writerFor(three, 'codex')).toBe('codex');
  expect(writerFor(three, 'gemini')).toBeUndefined();
  expect(writerFor(three, '')).toBeUndefined();
  expect(writerFor(null, 'claude')).toBeUndefined();
  // Listed but unable to write is still named: the PC falls back to an asked AI that can.
  expect(writerFor(infoOf([ai('claude', ['ask'], false)]), 'claude')).toBe('claude');
  expect(synthRequest(three, { on: true, agent: 'opencode' })).toEqual({ agent: 'opencode' });
  expect(synthRequest(three, { on: true, agent: 'gemini' })).toEqual({});
  expect(synthRequest(three, { on: false, agent: 'opencode' })).toEqual({ off: true });
  // 11-06: the moderator model set for the writer goes with it; set for another AI, it does not.
  const models = { opencode: { model: 'opencode-go/deepseek-v4-pro', effort: 'max' }, claude: { model: 'opus' } };
  expect(synthRequest(three, { on: true, agent: 'opencode', models })).toEqual({ agent: 'opencode', model: 'opencode-go/deepseek-v4-pro', effort: 'max' });
  expect(synthRequest(three, { on: true, agent: 'claude', models })).toEqual({ agent: 'claude', model: 'opus' });
  expect(synthRequest(three, { on: true, agent: 'gemini', models })).toEqual({});
  expect(synthRequest(three, { on: true, agent: 'codex', models })).toEqual({ agent: 'codex' });
});

test('comparing: the comparison runs; a done or failed one whose answer is being written is not comparing', () => {
  for (const status of ['waiting', 'running']) {
    expect(comparing({ status })).toBe(true);
    expect(comparing({ status, pollAfterMs: 1000 })).toBe(true);
  }
  for (const status of ['done', 'failed']) {
    expect(comparing({ status, pollAfterMs: 1000 })).toBe(false);
    expect(polling({ status, pollAfterMs: 1000 })).toBe(true);
    expect(comparing({ status })).toBe(false);
  }
  expect(comparing({ status: 'paused', pollAfterMs: 500 })).toBe(true);
  expect(comparing({ status: 'archived' })).toBe(false);
});

test('compareState: a done comparison whose combined answer is being written is done; a stopped one is retried once the writing ended', () => {
  const writing = withSyn(syn({ status: 'running', agent: 'claude' }));
  expect(polling(writing)).toBe(true);
  expect(compareState(synTurn(writing))).toEqual({ kind: 'done' });
  expect(compareState(synTurn(withSyn(doneSyn)))).toEqual({ kind: 'done' });
  // Failed (stopped): 다시 비교 only once the writing ended (the PC says 409 before).
  const stopped = (y: Synthesis) => withSyn(y, { status: 'failed', error: { code: 'cancelled' } });
  expect(canRetry(stopped(syn({ status: 'running' })))).toBe(false);
  expect(compareState(synTurn(stopped(syn({ status: 'running' }))))).toMatchObject({ kind: 'failed', action: null });
  expect(compareState(synTurn(stopped(syn({ text: 'x' }))))).toMatchObject({ kind: 'failed', action: 'retry' });
  // The comparison still runs: comparing, whatever the synthesis says.
  expect(compareState(synTurn(withSyn(syn({ status: 'waiting' }), { status: 'running', pollAfterMs: 1000 })))).toEqual({ kind: 'comparing' });
});

test('synthState: waiting, writing, done, failed and off', () => {
  expect(synthState(synTurn(withSyn(syn({ status: 'waiting', agent: 'codex' }), { status: 'running', pollAfterMs: 1000 })), three)).toEqual({ kind: 'waiting' });
  expect(synthState(synTurn(withSyn(syn({ status: 'waiting' }))), three)).toEqual({ kind: 'waiting' });
  expect(synthState(synTurn(withSyn(syn({ status: 'running', agent: 'claude' }))), three)).toEqual({ kind: 'writing' });
  expect(synthState(synTurn(withSyn(doneSyn)), three)).toEqual({
    kind: 'done',
    text: '# Seoul\n\nThe AIs differ.',
    agent: 'claude',
    model: 'sonnet',
    ms: 15230,
  });
  expect(synthState(synTurn(withSyn(syn({ agent: 'codex' }))), three)).toEqual({ kind: 'done', text: '', agent: 'codex' });

  const failed = syn({ status: 'failed', agent: 'claude', error: { code: 'judge_failed', message: 'usage limit reached' } });
  expect(synthState(synTurn(withSyn(failed)), three)).toEqual({ kind: 'failed', code: 'judge_failed', message: 'usage limit reached', retry: true });
  expect(synthState(synTurn(withSyn(syn({ status: 'failed' }))), three)).toEqual({ kind: 'failed', code: 'internal', retry: true });
  expect(synthState(synTurn(withSyn(syn({ status: 'failed', error: { code: 'no_judge' } }))), three)).toMatchObject({ code: 'no_judge', retry: true });
  // Failed while the comparison still runs (cancelled): writing again waits for it.
  const cancelled = withSyn(syn({ status: 'failed', error: { code: 'cancelled' } }), { status: 'running', pollAfterMs: 1000 });
  expect(synthState(synTurn(cancelled), three)).toEqual({ kind: 'failed', code: 'cancelled', retry: false });

  // Off: 'Write a combined answer' once the comparison is final, on a PC that can write.
  const off = syn({ status: 'off' });
  expect(synthState(synTurn(withSyn(off), { synth: { off: true } }), three)).toEqual({ kind: 'off', cost: 1 });
  expect(synthState(synTurn(withSyn(off, { status: 'failed', error: { code: 'cancelled' } })), three)).toEqual({ kind: 'off', cost: 1 });
  expect(synthState(synTurn(withSyn(off, { status: 'running', pollAfterMs: 1000 })), three)).toBeNull();
  expect(synthState(synTurn(withSyn(off)), askOnly)).toBeNull();
  expect(synthState(synTurn(withSyn(off)), null)).toBeNull();
});

test('synthState: nothing to show', () => {
  // An older agent: no synthesis in the snapshot.
  expect(synthState(synTurn(snapshot()), three)).toBeNull();
  // No snapshot, or one of another comparison.
  expect(synthState(doneTurn({ id: 'c1' }), three)).toBeNull();
  expect(synthState(doneTurn({ id: 'c2', snap: withSyn(doneSyn) }), three)).toBeNull();
  expect(synthState(turnOf(), three)).toBeNull();
  // An error on the turn takes the status line; a direct comparison on its way.
  expect(synthState(synTurn(withSyn(doneSyn), { error: 'network' }), three)).toBeNull();
  expect(synthState(synTurn(withSyn(doneSyn), { error: 'internal' }), three)).toBeNull();
  expect(synthState(synTurn(withSyn(doneSyn), { key: 'k', snap: undefined }), three)).toBeNull();
  // Not comparing, done or failed: still answering, or one answer.
  expect(synthState({ ...synTurn(withSyn(doneSyn)), run: runOf('running', answers3) }, three)).toBeNull();
  const two = withSyn(syn({ status: 'failed', error: { code: 'need_two_answers' } }), { status: 'failed', error: { code: 'need_two_answers' } });
  expect(synthState(synTurn(two), three)).toBeNull();
  // Too few answers for the writer alone, or a status this app does not know.
  expect(synthState(synTurn(withSyn(syn({ status: 'failed', error: { code: 'need_two_answers' } }))), three)).toBeNull();
  expect(synthState(synTurn(withSyn(syn({ status: 'paused' }))), three)).toBeNull();
});

// C6d (#24): conversations kept before C6 hold snapshots with the judging's
// answers, requests, judgments, items and counts, and a synthesis with
// judged, unnamedSides and topics. The phone reads none of it.
test('a snapshot kept from before C6, judging and all, still gives its state and combined answer', () => {
  const old = JSON.parse(
    JSON.stringify({
      ...snapshot(),
      answers: [{ id: 'claude', agent: 'claude', via: 'pc', family: 'anthropic', status: 'ready', notJudged: 0, units: [{ id: 'claude:1', n: 1, kind: 'sentence', text: 'x', start: 0, end: 1, judged: true }] }],
      requests: [{ id: 'r1', from: 'claude', against: 'codex', judge: 'opencode', judgeOnly: false, status: 'failed', attempt: 1, error: { code: 'judge_failed' } }],
      judgments: [{ unit: 'claude:1', against: 'codex', request: 'r1', judge: 'opencode', core: true, label: 'opposite', evidence: [], unsure: false, flags: [] }],
      items: [{ key: 'd:1', kind: 'differ', positions: [{ agents: ['claude'], units: ['claude:1'] }], check: [], missing: [] }],
      counts: { differ: 1, check: 0, agree: 0, onlyOne: 0, notCompared: 0, notJudged: 0 },
      synthesis: { ...doneSyn, judged: true, unnamedSides: 2, topics: { status: 'done', items: [{ key: 'd:1', topic: 'Population', gists: ['9.4'] }] } },
    }),
  ) as Comparison;
  const turn = synTurn(old);
  expect(compareState(turn)).toEqual({ kind: 'done' });
  expect(canRetry(old)).toBe(false);
  expect(synthState(turn, three)).toEqual({ kind: 'done', text: '# Seoul\n\nThe AIs differ.', agent: 'claude', model: 'sonnet', ms: 15230 });
});

test('synthErrorText: its own words, then the comparison words; judge_failed adds the AI words', () => {
  expect(synthErrorText('judge_failed', 'usage limit reached')).toBe('The writing AI reported an error.\nusage limit reached');
  expect(synthErrorText('judge_failed')).toBe('The writing AI reported an error.');
  expect(synthErrorText('judge_timeout', 'ignored')).toBe('The combined answer did not finish in time.');
  expect(synthErrorText('no_judge')).toBe('No AI could write it: it takes an AI that got this question and can write combined answers on this PC.');
  expect(synthErrorText('internal')).toBe('Something went wrong on the PC while writing the combined answer.');
  // Codes without words of their own: the comparison's, then the ask's and the general ones.
  expect(synthErrorText('need_two_answers')).toBe('Comparing needs at least two answers.');
  expect(synthErrorText('comparison_running')).toBe('Still comparing.');
  expect(synthErrorText('unauthorized')).toBe('This phone is no longer paired with the PC. Pair again.');
  expect(synthErrorText('brand_new')).toBe('Something went wrong (brand_new).');
});

test('synthBy: who wrote it, with what and how long', () => {
  expect(synthBy({ agent: 'claude', model: 'sonnet', ms: 15230 })).toBe('Claude sonnet · 15.2 s');
  expect(synthBy({ agent: 'opencode', ms: 900 })).toBe('OpenCode · 0.9 s');
  expect(synthBy({ agent: 'codex', model: 'gpt-6.1-terra' })).toBe('Codex gpt-6.1-terra');
  expect(synthBy({ agent: 'gemini' })).toBe('gemini');
  setLang('ko');
  try {
    expect(synthBy({ agent: 'claude', model: 'sonnet', ms: 15230 })).toBe('Claude sonnet · 15.2초');
  } finally {
    setLang('en');
  }
});
