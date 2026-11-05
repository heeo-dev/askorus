// Answer comparison on the phone (P2-3): who judges, what a turn sends, the
// state the 정리 (Summary) tab shows, and the cards drawn from the PC's
// snapshot (GET /v1/comparisons/{id}). Everything here is pure: the screen
// draws what these give and thread.ts follows the comparison.
//
// The snapshot is the PC's word. The phone only orders, drops what it cannot
// show (an unknown card kind, a unit it cannot find) and picks the words;
// unknown codes from a newer agent still show as their code. Item keys pass
// through unchanged, so a card (and an open sheet) stays put while judges
// finish. Lengths the agent limits are UTF-8 bytes: measure them with
// utf8Bytes and cutUtf8, never with .length.
//
// S5b: once the judging ended, one AI may write the answers into one
// combined answer (the snapshot's synthesis). It has its own status: the
// comparison's status stays the judges', and the PC keeps pollAfterMs while
// the writing is not done. With a combined answer the cards fold under it
// as evidence; without one (off, failed, an older agent) they show as in S5.

import { agentName } from '@/api/agents';
import { AgentError, CancelledError, NetworkError, comparisonPolling } from '@/api/client';
import type {
  AdapterInfo,
  CodeError,
  CompareAnswer,
  Comparison,
  ComparisonAnswer,
  ComparisonRequest,
  DifferTopic,
  Info,
  Judgment,
  NumberSpan,
  RefineRequest,
  Run,
  RunPart,
  Synthesis,
  SynthesisRequest,
  UnitMath,
} from '@/api/types';
import type { Target, Turn, TurnCompare } from '@/ask/thread';
import { has, type MessageKey, t } from '@/i18n';
import { texOfSpan } from '@/ui/inlineMath';
import { features } from './features';

/** The cards of the Summary tab, always in this order. */
export const cardKinds = ['differ', 'check', 'agree', 'onlyOne'] as const;
export type CardKind = (typeof cardKinds)[number];

/** Checks the agent may report on an item (why it needs a look). */
export const checkNames = ['unsure', 'noEvidence', 'crossDirection', 'numberMismatch', 'numbersAbsent'] as const;

/** Judgment labels (labels v1). */
export const labels = ['same', 'opposite', 'absent'] as const;

/** Error codes of comparisons, each with its own words (compare.error.*). */
export const compareCodes = [
  'judge_failed',
  'judge_timeout',
  'judge_bad_output',
  'judge_used_tool',
  'bad_model',
  'agent_unavailable',
  'no_judge',
  'need_two_answers',
  'too_many_comparisons',
  'cancelled',
  'internal',
  'comparison_not_found',
  'comparison_running',
  'bad_compare',
  'bad_request',
] as const;

/** Error codes of a combined answer with words of their own (compare.synth.error.*); others take the compare words. */
export const synthCodes = [
  'judge_failed',
  'judge_timeout',
  'judge_bad_output',
  'judge_used_tool',
  'bad_model',
  'agent_unavailable',
  'no_judge',
  'cancelled',
  'internal',
] as const;

/** Items shown in a card before 'Show n more'. */
export const firstShown = 3;
// The agent's limits (POST /v1/runs compare, POST /v1/comparisons).
export const maxEarlier = 3;
export const maxEarlierBytes = 4096;
export const maxAnswerBytes = 65536;
export const maxQuestionBytes = 32768;

// Codes after which the screen offers a direct comparison (비교하기).
const startAfter = ['too_many_comparisons', 'internal', 'bad_compare', 'ask_folder_unavailable'];
// Codes a retry cannot change.
const noRetry = ['no_judge', 'need_two_answers'];

// ---- Lengths (UTF-8 bytes, as the agent counts them) ----

const high = (c: number) => c >= 0xd800 && c <= 0xdbff;
const low = (c: number) => c >= 0xdc00 && c <= 0xdfff;

// bytesAt gives the UTF-8 length of the code point at s[i] and how many
// UTF-16 units it takes. A lone surrogate goes out as U+FFFD (3 bytes).
function bytesAt(s: string, i: number): [bytes: number, units: number] {
  const c = s.charCodeAt(i);
  if (c < 0x80) return [1, 1];
  if (c < 0x800) return [2, 1];
  if (high(c) && i + 1 < s.length && low(s.charCodeAt(i + 1))) return [4, 2];
  return [3, 1];
}

/** utf8Bytes is the length of s in UTF-8, as the agent measures it. */
export function utf8Bytes(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; ) {
    const [b, u] = bytesAt(s, i);
    n += b;
    i += u;
  }
  return n;
}

/** cutUtf8 is the longest start of s of whole characters within maxBytes of UTF-8. */
export function cutUtf8(s: string, maxBytes: number): string {
  if (s.length * 3 <= maxBytes) return s;
  let n = 0;
  for (let i = 0; i < s.length; ) {
    const [b, u] = bytesAt(s, i);
    if (n + b > maxBytes) return s.slice(0, i);
    n += b;
    i += u;
  }
  return s;
}

// ---- Who judges, and what a turn sends ----

const canJudge = (a: AdapterInfo) => a.available && (a.capabilities ?? []).includes('judge');

const canWrite = (a: AdapterInfo) => a.available && (a.capabilities ?? []).includes('synthesize');

/** canSynthesize: some AI of this PC can write combined answers (S5b); an older agent has none. */
export function canSynthesize(info: Info | null | undefined): boolean {
  return (info?.agents ?? []).some(canWrite);
}

/**
 * writerFor gives the AI to ask for as the writer: the one the person
 * picked when the PC lists it, else undefined (the PC picks). Asking for an
 * AI the PC does not have would make it refuse the whole comparison.
 */
export function writerFor(info: Info | null | undefined, agent: string): string | undefined {
  return agent && (info?.agents ?? []).some((a) => a.agent === agent) ? agent : undefined;
}

/** synthRequest is what a turn asks for: the writer (or the PC's pick), or off. */
export function synthRequest(info: Info, o: { on: boolean; agent: string }): SynthesisRequest {
  if (!o.on) return { off: true };
  const agent = writerFor(info, o.agent);
  return agent ? { agent } : {};
}

/**
 * refineRequest is what a turn asks for to refine its question (milestone
 * C): the moderator is the writer the person picked (or the PC's pick), on
 * a PC whose AIs can write; nothing when refining is off or no AI can.
 */
export function refineRequest(info: Info, o: { on: boolean; agent: string }): RefineRequest | undefined {
  if (!o.on || !canSynthesize(info)) return undefined;
  const agent = writerFor(info, o.agent);
  return agent ? { agent } : {};
}

/** judgesOf gives the asked AIs (in asked order) that can judge on this PC. */
export function judgesOf(info: Info, asked: string[]): string[] {
  return asked.filter((agent) => (info.agents ?? []).some((a) => a.agent === agent && canJudge(a)));
}

/**
 * compareStart decides at send time whether a turn gets a Summary tab and
 * how it compares. null keeps the turn as in P2-2: a follow-up to one AI, a
 * one-AI turn, or a PC where no AI judges (an older agent too). Two AIs
 * cannot judge each other (the judge must not have written either answer),
 * so a two-AI turn shows why instead.
 */
export function compareStart(o: { info: Info; asked: string[]; to: Target; auto: boolean }): TurnCompare['start'] | null {
  if (o.to !== 'all' || o.asked.length < 2) return null;
  // C6: no judging, so two AIs are compared too, always as the answers
  // come in; the combined answer's own switch says whether it is written.
  if (!features.judging) return canSynthesize(o.info) ? 'auto' : null;
  const pcJudges = (o.info.agents ?? []).filter(canJudge);
  if (!pcJudges.length) return null;
  if (o.asked.length === 2) {
    const off = pcJudges.some((a) => (a.capabilities ?? []).includes('ask') && !o.asked.includes(a.agent));
    return off ? 'noJudgeOff' : 'noJudgeNone';
  }
  if (!judgesOf(o.info, o.asked).length) return null;
  return o.auto ? 'auto' : 'manual';
}

const doneWith = (turn: Turn, agent: string) => (turn.run?.parts ?? []).some((p) => p.agent === agent && p.status === 'done');

/**
 * earlierFor gives the earlier questions every judge's company already got
 * (each judge answered them), so the judges read this answer in its
 * conversation: the latest three, oldest first, each cut to 4 KiB.
 */
export function earlierFor(turns: Turn[], judges: string[]): string[] {
  if (!judges.length) return [];
  return turns
    .filter((x) => judges.every((j) => doneWith(x, j)))
    .slice(-maxEarlier)
    .map((x) => cutUtf8(x.question.replace(/\u0000/g, ''), maxEarlierBytes));
}

/** readyParts are the turn's answers that can be compared, in asked order. */
export function readyParts(turn: Turn): RunPart[] {
  const out: RunPart[] = [];
  for (const agent of turn.agents) {
    const p = (turn.run?.parts ?? []).find((x) => x.agent === agent);
    if (p && p.status === 'done' && p.answer?.trim()) out.push(p);
  }
  return out;
}

/**
 * directRequest is the POST /v1/comparisons body for a turn (비교하기, or
 * 다시 비교 after the PC lost the comparison): the answers that came, each
 * cut to the agent's 64 KiB, with the judges and earlier questions fixed
 * when the turn was sent. null when there are not two answers.
 */
export function directRequest(turn: Turn, key: string): ComparisonRequest | null {
  const answers: ComparisonAnswer[] = readyParts(turn).map((p) => {
    const a: ComparisonAnswer = { id: p.agent, agent: p.agent, via: 'pc', text: cutUtf8(p.answer ?? '', maxAnswerBytes) };
    if (p.model) a.model = p.model;
    return a;
  });
  if (answers.length < 2) return null;
  const req: ComparisonRequest = {
    clientComparisonId: key,
    question: cutUtf8(turn.question, maxQuestionBytes),
    earlier: turn.compare?.earlier ?? [],
    judges: turn.compare?.judges ?? [],
    answers,
  };
  // The combined answer as fixed at send time, with the question's tier.
  const synth = turn.compare?.synth;
  if (synth) req.synthesis = turn.req.tier ? { ...synth, tier: turn.req.tier } : { ...synth };
  return req;
}

// synthAsked: the turn asks for a combined answer (fixed at send time).
const synthAsked = (turn: Turn) => !!turn.compare?.synth && !turn.compare.synth.off;

/**
 * startCost is how many more times 비교하기 uses the PC's AIs: every
 * ordered pair of answers once; two answers need a judge that wrote none.
 * A combined answer asked for adds one more. 0: nothing to start.
 */
export function startCost(turn: Turn): number {
  const ready = readyParts(turn);
  const k = ready.length;
  let n = 0;
  if (k >= 3) n = k * (k - 1);
  else if (k === 2 && (turn.compare?.judges ?? []).some((j) => !ready.some((p) => p.agent === j))) n = 2;
  return n > 0 && synthAsked(turn) ? n + 1 : n;
}

// ---- State updates (the same object back when nothing changes) ----

/**
 * withRun takes the run's comparison: its id, only while the turn has none
 * (a direct comparison made after a 404 is never replaced by the run's old
 * id), or why the PC did not take the compare. It never clears either.
 */
export function withRun(c: TurnCompare | undefined, run: Run): TurnCompare | undefined {
  if (!c) return c;
  let next = c;
  if (!next.id && run.comparisonId) next = { ...next, id: run.comparisonId };
  if (!next.id && !next.error && run.compareError?.code) next = { ...next, error: run.compareError.code };
  return next;
}

/** withSnap keeps the latest snapshot; an answer from the PC clears an earlier error. */
export function withSnap(c: TurnCompare, s: Comparison): TurnCompare {
  return { ...c, id: s.comparisonId, snap: s, error: undefined };
}

/** errorCode: the agent's code, 'network' when the PC did not answer, else 'internal'. */
export function errorCode(e: unknown): string {
  if (e instanceof AgentError) return e.code;
  if (e instanceof NetworkError) return 'network';
  return 'internal';
}

/** withError records why following failed; stopping (CancelledError) is not an error. */
export function withError(c: TurnCompare, e: unknown): TurnCompare {
  if (e instanceof CancelledError) return c;
  return { ...c, error: errorCode(e) };
}

/** polling: the PC wants to be asked again (also for a status this app does not know). */
export function polling(s: Pick<Comparison, 'status' | 'pollAfterMs'>): boolean {
  return comparisonPolling(s);
}

export function isFinal(s: Pick<Comparison, 'status' | 'pollAfterMs'>): boolean {
  return !polling(s);
}

/**
 * judging: the judges still run. Unlike polling, a done or failed
 * comparison whose combined answer is still being written is not judging;
 * a status this app does not know is judging while the PC asks to be polled.
 */
export function judging(s: Pick<Comparison, 'status' | 'pollAfterMs'>): boolean {
  if (s.status === 'waiting' || s.status === 'running') return true;
  if (s.status === 'done' || s.status === 'failed') return false;
  return (s.pollAfterMs ?? 0) > 0;
}

const judgedFailed = (s: Comparison) => (s.requests ?? []).some((r) => r.status === 'failed' && r.judge !== '');

/** canRetry: a final comparison with judgments that failed, or that failed as a whole for a reason a retry may fix. */
export function canRetry(s?: Comparison): boolean {
  if (!s || polling(s)) return false;
  return judgedFailed(s) || (s.status === 'failed' && !noRetry.includes(s.error?.code ?? ''));
}

/** progress counts the judgments that ended; no_judge records are not judgments. */
export function progress(s: Comparison): { ended: number; total: number } {
  const judged = (s.requests ?? []).filter((r) => r.judge !== '');
  return { ended: judged.filter((r) => r.status === 'done' || r.status === 'failed').length, total: judged.length };
}

// ---- What the Summary tab shows ----

export type CompareState =
  | { kind: 'answering' }
  | { kind: 'manual'; cost: number }
  | { kind: 'noJudge'; why: 'off' | 'none' }
  | { kind: 'oneAnswer' }
  | { kind: 'comparing'; ended: number; total: number }
  | { kind: 'done'; partial: boolean; retry: boolean }
  | { kind: 'failed'; code: string; message?: string; action: 'retry' | 'start' | null }
  | { kind: 'notFound' }
  | { kind: 'lost'; action: 'start' | 'poll' };

/** compareState is the Summary tab's state; null for a turn without one. */
export function compareState(turn: Turn): CompareState | null {
  const c = turn.compare;
  if (!c) return null;
  if (c.start === 'noJudgeOff') return { kind: 'noJudge', why: 'off' };
  if (c.start === 'noJudgeNone') return { kind: 'noJudge', why: 'none' };
  if (!turn.run || turn.run.status === 'running') return { kind: 'answering' };
  const ready = readyParts(turn).length;
  if (c.error) {
    if (c.error === 'network') return { kind: 'lost', action: c.key ? 'start' : 'poll' };
    if (c.error === 'comparison_not_found') return { kind: 'notFound' };
    if (c.error === 'need_two_answers') return { kind: 'oneAnswer' };
    return { kind: 'failed', code: c.error, action: startAfter.includes(c.error) && ready >= 2 ? 'start' : null };
  }
  if (ready < 2) return { kind: 'oneAnswer' };
  // A direct comparison on its way to the PC (also one replacing a lost one).
  if (c.key) return { kind: 'comparing', ended: 0, total: 0 };
  if (!c.id) {
    // No pair has a judge (two answers, each judge wrote one): Compare could only fail.
    const cost = startCost(turn);
    return cost > 0 ? { kind: 'manual', cost } : { kind: 'failed', code: 'no_judge', action: null };
  }
  const s = c.snap;
  if (!s || s.comparisonId !== c.id) return { kind: 'comparing', ended: 0, total: 0 };
  if (judging(s)) return { kind: 'comparing', ...progress(s) };
  if (s.status === 'failed') {
    if (s.error?.code === 'need_two_answers') return { kind: 'oneAnswer' };
    return { kind: 'failed', code: s.error?.code ?? 'internal', message: s.error?.message, action: canRetry(s) ? 'retry' : null };
  }
  return { kind: 'done', partial: judgedFailed(s), retry: canRetry(s) };
}

// ---- Words ----

/**
 * compareErrorText explains a comparison error: its compare words, else the
 * ask words, else the general ones, else the code. A judging AI's own
 * message follows on the next line.
 */
export function compareErrorText(code: string, message?: string): string {
  const own = `compare.error.${code}`;
  const ask = `ask.error.${code}`;
  const general = `error.${code}`;
  const base = has(own)
    ? t(own)
    : has(ask)
      ? t(ask, { folder: message ?? '' })
      : has(general)
        ? t(general)
        : t('error.unknown', { code });
  return code === 'judge_failed' && message ? `${base}\n${message}` : base;
}

// ---- The combined answer (S5b) ----

export type SynthState =
  | { kind: 'waiting' }
  | { kind: 'writing' }
  | { kind: 'done'; text: string; agent: string; model?: string; ms?: number; judged: boolean; unnamed: number; topics?: DifferTopic[] }
  | { kind: 'failed'; code: string; message?: string; retry: boolean }
  | { kind: 'off'; cost: 1 };

// The turn's latest snapshot when it is of the turn's comparison.
const snapOf = (turn: Turn) => {
  const c = turn.compare;
  return c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
};

/**
 * synthState is the combined answer the Summary tab shows; null shows none
 * and leaves the cards open as in S5: no snapshot of this comparison, a
 * snapshot without synthesis (an older agent), an error on the turn, a
 * Summary tab that is not comparing, done or failed, a status this app
 * does not know, or too few answers. Off offers 'Write a combined answer'
 * only once the comparison is final, on a PC that can write. A failed one
 * may be written again once the comparison is final (else the PC says 409).
 */
export function synthState(turn: Turn, info: Info | null): SynthState | null {
  const s = snapOf(turn);
  const y = s?.synthesis;
  if (!s || !y || turn.compare?.error) return null;
  const kind = compareState(turn)?.kind;
  if (kind !== 'comparing' && kind !== 'done' && kind !== 'failed') return null;
  switch (y.status) {
    case 'waiting':
      return { kind: 'waiting' };
    case 'running':
      return { kind: 'writing' };
    case 'done': {
      const out: SynthState = {
        kind: 'done',
        text: y.text ?? '',
        agent: y.agent ?? '',
        judged: !!y.judged,
        unnamed: Math.max(0, y.unnamedSides ?? 0),
      };
      if (y.model) out.model = y.model;
      if (y.ms !== undefined) out.ms = y.ms;
      if (y.topics?.status === 'done' && Array.isArray(y.topics.items)) out.topics = y.topics.items;
      return out;
    }
    case 'failed': {
      const code = y.error?.code || 'internal';
      if (code === 'need_two_answers') return null;
      const out: SynthState = { kind: 'failed', code, retry: isFinal(s) };
      if (y.error?.message) out.message = y.error.message;
      return out;
    }
    case 'off':
      return isFinal(s) && canSynthesize(info) ? { kind: 'off', cost: 1 } : null;
    default:
      return null;
  }
}

/** evidenceFolded: the cards fold under a combined answer that is coming or came. */
export function evidenceFolded(st: SynthState | null): boolean {
  return st?.kind === 'waiting' || st?.kind === 'writing' || st?.kind === 'done';
}

/**
 * synthOn: the writing AI's company gets the answers, so the footer says
 * so: the snapshot's synthesis is not off, or before a snapshot the turn
 * asks for one. A snapshot without synthesis (an older agent) writes none.
 */
export function synthOn(turn: Turn): boolean {
  const s = snapOf(turn);
  if (s) return !!s.synthesis && s.synthesis.status !== 'off';
  return synthAsked(turn);
}

/**
 * synthErrorText explains why a combined answer failed: its own words,
 * else the comparison's (compareErrorText). The writing AI's own message
 * follows judge_failed on the next line.
 */
export function synthErrorText(code: string, message?: string): string {
  const own = `compare.synth.error.${code}`;
  if (!has(own)) return compareErrorText(code, message);
  const base = t(own);
  return code === 'judge_failed' && message ? `${base}\n${message}` : base;
}

/** synthBy: who wrote it, with what and how long: "Claude sonnet · 15.2 s". */
export function synthBy(o: { agent: string; model?: string; ms?: number }): string {
  const who = [agentName(o.agent), o.model ?? ''].filter(Boolean).join(' ');
  const time = o.ms !== undefined ? t('ask.seconds', { s: (o.ms / 1000).toFixed(1) }) : '';
  return [who, time].filter(Boolean).join(' · ');
}

/** synthLine is the combined answer's line in the Judged by sheet; null when there is none to show. */
export function synthLine(s: Comparison | undefined): { what: string; text: string; failed?: boolean } | null {
  const y: Synthesis | undefined = s?.synthesis;
  if (!y || y.status === 'off') return null;
  const what = y.agent ? t('compare.synth.line', { name: agentName(y.agent) }) : t('compare.synth.title');
  switch (y.status) {
    case 'waiting':
      return { what, text: t('compare.req.queued') };
    case 'running':
      return { what, text: t('compare.synth.writing') };
    case 'done': {
      const time = y.ms !== undefined ? t('ask.seconds', { s: (y.ms / 1000).toFixed(1) }) : '';
      return { what, text: [time, y.model ?? ''].filter(Boolean).join(' · ') };
    }
    case 'failed':
      return { what, text: synthErrorText(y.error?.code || 'internal', y.error?.message), failed: true };
    default:
      return { what, text: y.status };
  }
}

/** whyText: why an item needs a look, or null for a check this app does not know. */
export function whyText(check: string): string | null {
  const key = `compare.why.${check}`;
  return has(key) ? t(key) : null;
}

/** labelText: a judgment label in words, or its code when unknown. */
export function labelText(label: string): string {
  const key = `compare.label.${label}`;
  return has(key) ? t(key as MessageKey) : label;
}

// ---- Tabs and the lines above them ----

export type TabView = { key: string /* 'summary' | agent */; agent?: string; mark: '' | '…' | '✕' };

/** tabs: Summary first, then one per asked AI with how its answer stands. null: no Summary tab. */
export function tabs(turn: Turn): TabView[] | null {
  if (!turn.compare) return null;
  const out: TabView[] = [{ key: 'summary', mark: '' }];
  for (const agent of turn.agents) {
    const p = (turn.run?.parts ?? []).find((x) => x.agent === agent);
    const mark = !p || p.status === 'running' ? (turn.error ? '✕' : '…') : p.status === 'done' ? '' : '✕';
    out.push({ key: agent, agent, mark });
  }
  return out;
}

/** answered: how many AIs answered, and how many of them on this PC (all, in P2-3). */
export function answered(turn: Turn): { n: number; pc: number } {
  const n = (turn.run?.parts ?? []).filter((p) => turn.agents.includes(p.agent) && p.status === 'done').length;
  return { n, pc: n };
}

/** answeringLines: the AIs (display names) still answering and for how many seconds (redrawn by the polls). */
export function answeringLines(turn: Turn, now: number): { agent: string; s: number }[] {
  if (turn.error) return [];
  const from = turn.at ?? now;
  const s = Math.max(0, Math.floor((now - from) / 1000));
  return turn.agents
    .filter((agent) => {
      const p = (turn.run?.parts ?? []).find((x) => x.agent === agent);
      return !p || p.status === 'running';
    })
    .map((agent) => ({ agent: agentName(agent), s }));
}

// ---- Cards ----

/** One unit to draw. answer is the answer id; agent is the AI's display name. */
export type UnitView = { id: string; answer: string; agent: string; kind: string; text: string; context?: string; math: UnitMath[] };
/** One side of an item: the AIs that say it (display names) and their units; pos is its place among the item's positions. */
export type SideView = { agents: string[]; units: UnitView[]; numberSpans: NumberSpan[]; pos?: number };
/**
 * missing: the AIs (display names) whose answers this item could not be
 * compared with. whole: the AIs (display names) of the sides that are a
 * whole answer, with no sentence (an opposite no evidence pointed into);
 * they are not drawn as sides but still named and counted.
 */
export type ItemView = { key: string; kind: CardKind; sides: SideView[]; numbersDiffer: boolean; check: string[]; missing: string[]; whole: string[] };
export type CardView = { kind: CardKind; items: ItemView[] };

const isCardKind = (k: string): k is CardKind => (cardKinds as readonly string[]).includes(k);

// Lookups over a snapshot's answers and units.
function index(s: Comparison) {
  const answers = new Map<string, CompareAnswer>();
  const unitAnswer = new Map<string, CompareAnswer>();
  const units = new Map<string, UnitView>();
  for (const a of s.answers ?? []) {
    answers.set(a.id, a);
    for (const u of a.units ?? []) {
      unitAnswer.set(u.id, a);
      const v: UnitView = { id: u.id, answer: a.id, agent: agentName(a.agent), kind: u.kind, text: u.text ?? '', math: u.math ?? [] };
      if (u.context) v.context = u.context;
      units.set(u.id, v);
    }
  }
  // An answer id as the AI's name; an id the snapshot does not know shows as it is.
  const nameOf = (id: string) => {
    const a = answers.get(id);
    return a ? agentName(a.agent) : id;
  };
  return { answers, unitAnswer, units, nameOf };
}

/**
 * cards are the four cards in their fixed order, items in the snapshot's
 * order. What cannot be shown is left out: an unknown kind, a repeated key
 * (the first stays), units no answer has, sides none of whose units are
 * known, items without sides. A side with no units at all is a whole
 * answer: its AIs go to the item's whole (COR-1).
 */
export function cards(s: Comparison): CardView[] {
  const { units, nameOf } = index(s);
  const byKind = new Map<CardKind, ItemView[]>(cardKinds.map((k) => [k, []]));
  const seen = new Set<string>();
  for (const it of s.items ?? []) {
    if (!isCardKind(it.kind) || seen.has(it.key)) continue;
    const sides: SideView[] = [];
    const whole: string[] = [];
    for (const [pos, p] of (it.positions ?? []).entries()) {
      const us = (p.units ?? []).map((id) => units.get(id)).filter((u): u is UnitView => !!u);
      if (us.length) sides.push({ agents: (p.agents ?? []).map(nameOf), units: us, numberSpans: p.numberSpans ?? [], pos });
      else if (!(p.units ?? []).length) whole.push(...(p.agents ?? []).map(nameOf));
    }
    if (!sides.length) continue;
    seen.add(it.key);
    byKind.get(it.kind)?.push({
      key: it.key,
      kind: it.kind,
      sides,
      numbersDiffer: !!it.numbersDiffer,
      check: it.check ?? [],
      missing: (it.missing ?? []).map(nameOf),
      whole,
    });
  }
  return cardKinds.map((kind) => ({ kind, items: byKind.get(kind) ?? [] }));
}

/** chipCounts counts the items the cards show (not the snapshot's counts). */
export function chipCounts(cs: CardView[]): Record<CardKind, number> {
  const out = { differ: 0, check: 0, agree: 0, onlyOne: 0 };
  for (const c of cs) out[c.kind] = c.items.length;
  return out;
}

/**
 * shownCards: one card when a chip is picked; while judges still run only
 * Differ (the rest come when all ended); at the end Differ always (with its
 * no-differ sentence) and the others when they have items.
 */
export function shownCards(cs: CardView[], o: { only: CardKind | null; ended: boolean }): CardView[] {
  if (o.only) return cs.filter((c) => c.kind === o.only);
  if (!o.ended) return cs.filter((c) => c.kind === 'differ');
  return cs.filter((c) => c.kind === 'differ' || c.items.length > 0);
}

/**
 * usableJudgments leaves out judgments no one may use: of a unit or answer
 * the snapshot does not know, by the AI that wrote the unit or the answer
 * it was read against (저자 판정 버림), or of an answer against itself.
 */
export function usableJudgments(s: Comparison): Judgment[] {
  const { answers, unitAnswer } = index(s);
  return (s.judgments ?? []).filter((j) => {
    const from = unitAnswer.get(j.unit);
    const against = answers.get(j.against);
    if (!from || !against || from.id === against.id) return false;
    return j.judge !== from.agent && j.judge !== against.agent;
  });
}

// ---- Drawing a unit's text ----

export type Piece = { text: string; bold?: boolean; math?: { tex: string; display: boolean } };
type Span = { start: number; end: number };

/**
 * pieces splits a unit's text for drawing: math only at the spans the PC
 * gave (the text is never scanned for "$", S1 decision 1), bold where the
 * number spans are. Spans that do not fit the text are dropped (math) or
 * clamped (bold), an end inside an emoji is moved out of it, and math that
 * overlaps earlier math is dropped. A math piece keeps its marks as text,
 * for when the TeX cannot be drawn.
 */
export function pieces(text: string, math: UnitMath[] | undefined, bold: Span[]): Piece[] {
  const n = text.length;
  const inPair = (i: number) => i > 0 && i < n && high(text.charCodeAt(i - 1)) && low(text.charCodeAt(i));
  const snap = (sp: Span): Span => ({ start: inPair(sp.start) ? sp.start - 1 : sp.start, end: inPair(sp.end) ? sp.end + 1 : sp.end });
  const ints = (sp: Span) => Number.isInteger(sp.start) && Number.isInteger(sp.end);

  const maths: Array<Span & { tex: { tex: string; display: boolean } | null }> = [];
  const candidates = (math ?? [])
    .filter((m) => ints(m) && m.start >= 0 && m.start < m.end && m.end <= n)
    .map(snap)
    .sort((a, b) => a.start - b.start);
  for (const m of candidates) {
    if (maths.length && m.start < maths[maths.length - 1].end) continue;
    maths.push({ ...m, tex: texOfSpan(text.slice(m.start, m.end)) });
  }

  const clamp = (i: number) => Math.min(n, Math.max(0, i));
  const bolds: Span[] = [];
  for (const b of (bold ?? [])
    .filter(ints)
    .map((b) => snap({ start: clamp(b.start), end: clamp(b.end) }))
    .filter((b) => b.start < b.end)
    .sort((a, b) => a.start - b.start)) {
    const last = bolds[bolds.length - 1];
    if (last && b.start <= last.end) last.end = Math.max(last.end, b.end);
    else bolds.push({ ...b });
  }

  const out: Piece[] = [];
  const plain = (from: number, to: number) => {
    let at = from;
    for (const b of bolds) {
      if (b.end <= at || b.start >= to) continue;
      if (b.start > at) out.push({ text: text.slice(at, b.start) });
      const end = Math.min(b.end, to);
      out.push({ text: text.slice(Math.max(at, b.start), end), bold: true });
      at = end;
    }
    if (at < to) out.push({ text: text.slice(at, to) });
  };
  let at = 0;
  for (const m of maths) {
    plain(at, m.start);
    if (m.tex) {
      const p: Piece = { text: text.slice(m.start, m.end), math: m.tex };
      if (bolds.some((b) => b.start < m.end && b.end > m.start)) p.bold = true;
      out.push(p);
    } else {
      plain(m.start, m.end);
    }
    at = m.end;
  }
  plain(at, n);
  return out;
}

// ---- The sheets ----

/** One judgment of a unit, in display names; label is the code (labelText gives the words). */
export type JudgmentLine = { judge: string; from: string; against: string; unit: string; label: string; unsure: boolean };
/** why: the known checks of the item; agents: the AIs (codes, as the tab keys) whose answers the item quotes. */
export type PairView = { item: ItemView; lines: JudgmentLine[]; why: string[]; agents: string[] };

/** pairView is what the sheet of one item shows; null when the item is not in the snapshot. */
export function pairView(s: Comparison, key: string): PairView | null {
  const item = cards(s)
    .flatMap((c) => c.items)
    .find((i) => i.key === key);
  if (!item) return null;
  const { unitAnswer, nameOf } = index(s);
  const unitIds = new Set(item.sides.flatMap((sd) => sd.units.map((u) => u.id)));
  const lines: JudgmentLine[] = usableJudgments(s)
    .filter((j) => unitIds.has(j.unit))
    .map((j) => ({
      judge: agentName(j.judge),
      from: nameOf(unitAnswer.get(j.unit)?.id ?? ''),
      against: nameOf(j.against),
      unit: j.unit,
      label: j.label,
      unsure: !!j.unsure,
    }));
  const why = item.check.filter((c) => (checkNames as readonly string[]).includes(c));
  const agents: string[] = [];
  for (const sd of item.sides)
    for (const u of sd.units) {
      const a = unitAnswer.get(u.id)?.agent;
      if (a && !agents.includes(a)) agents.push(a);
    }
  return { item, lines, why, agents };
}

/** One judging request in display names; judge is '' for a pair no AI could judge (no_judge). */
export type RequestLine = {
  id: string;
  judge: string;
  from: string;
  against: string;
  judgeOnly: boolean;
  model?: string;
  status: string;
  ms?: number;
  error?: CodeError;
};

export function requestLines(s: Comparison): RequestLine[] {
  const { nameOf } = index(s);
  return (s.requests ?? []).map((r) => {
    const line: RequestLine = {
      id: r.id,
      judge: r.judge ? agentName(r.judge) : '',
      from: nameOf(r.from),
      against: nameOf(r.against),
      judgeOnly: !!r.judgeOnly,
      status: r.status,
    };
    if (r.model) line.model = r.model;
    if (r.ms !== undefined) line.ms = r.ms;
    if (r.error) line.error = r.error;
    return line;
  });
}

const unique = (xs: string[]) => xs.filter((x, i) => xs.indexOf(x) === i);

// inOrder sorts AI codes by their place in order (the turn's or the
// snapshot's answers), those not in it last as they came, so names read in
// the same order as the tabs.
function inOrder(codes: string[], order: string[]): string[] {
  const at = (x: string) => {
    const i = order.indexOf(x);
    return i < 0 ? order.length : i;
  };
  return codes.map((x, i) => ({ x, i })).sort((a, b) => at(a.x) - at(b.x) || a.i - b.i).map((v) => v.x);
}

/**
 * judgeModels: the judges (display names) and their models for the footer,
 * in the turn's order, from the snapshot's requests; before one, the turn's
 * judges with the models the PC reports.
 */
export function judgeModels(turn: Turn, info: Info | null): { agent: string; model?: string }[] {
  const c = turn.compare;
  const s = c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
  if (s && (s.requests ?? []).length) {
    const models = new Map<string, string | undefined>();
    for (const r of s.requests ?? []) if (r.judge && !models.has(r.judge)) models.set(r.judge, r.model || undefined);
    return inOrder([...models.keys()], turn.agents).map((j) => {
      const model = models.get(j);
      return model ? { agent: agentName(j), model } : { agent: agentName(j) };
    });
  }
  return (c?.judges ?? []).map((agent) => {
    const model = info?.agents.find((a) => a.agent === agent)?.judge?.model;
    return model ? { agent: agentName(agent), model } : { agent: agentName(agent) };
  });
}

/** judgedBy: who judged an answer's units (display names), in the snapshot's answer order. */
export function judgedBy(s: Comparison | undefined, answerId: string): string[] {
  if (!s) return [];
  // A judge whose judging of this answer failed did not judge it.
  const judges = unique((s.requests ?? []).filter((r) => r.from === answerId && r.judge && r.status !== 'failed').map((r) => r.judge));
  return unique(inOrder(judges, (s.answers ?? []).map((a) => a.agent)).map(agentName));
}

/** failedWith: the AIs (display names) in pairs whose judging failed, in answer order. */
export function failedWith(s: Comparison): string[] {
  const ids = new Set<string>();
  for (const r of s.requests ?? []) {
    if (r.status !== 'failed' || !r.judge) continue;
    ids.add(r.from);
    ids.add(r.against);
  }
  const { nameOf } = index(s);
  const ordered = (s.answers ?? []).map((a) => a.id).filter((id) => ids.has(id));
  for (const id of ids) if (!ordered.includes(id)) ordered.push(id);
  return unique(ordered.map(nameOf));
}

/** longAnswers: answers the PC compared only the start of, with how many units were judged. */
export function longAnswers(s: Comparison): { agent: string; judged: number }[] {
  return (s.answers ?? [])
    .filter((a) => (a.notJudged ?? 0) > 0)
    .map((a) => ({ agent: agentName(a.agent), judged: (a.units ?? []).filter((u) => u.judged).length }));
}
