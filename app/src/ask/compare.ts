// Answer comparison on the phone (P2-3): what a turn sends, the state of
// its comparison and its combined answer, from the PC's snapshot (GET
// /v1/comparisons/{id}). Everything here is pure: the screen draws what
// these give and thread.ts follows the comparison.
//
// Since C6 (#19, 2026-11-05, the user's decision) a comparison is only the
// moderator's combined answer: the PC judges no more, so a comparison is
// done as soon as the answers are in, and then the moderator writes (S5b,
// the snapshot's synthesis, with a status of its own; the PC keeps
// pollAfterMs while the writing is not done). The judging's cards, evidence
// and details left the app with C6d (#24). Conversations kept before it
// may still hold their snapshots: only what is read here matters.
//
// The snapshot is the PC's word. Unknown codes from a newer agent still
// show as their code. Lengths the agent limits are UTF-8 bytes: measure
// them with utf8Bytes and cutUtf8, never with .length.

import { agentName } from '@/api/agents';
import { AgentError, CancelledError, NetworkError, comparisonPolling } from '@/api/client';
import type {
  AdapterInfo,
  Comparison,
  ComparisonAnswer,
  ComparisonRequest,
  Info,
  RefineRequest,
  Run,
  RunPart,
  SynthesisRequest,
} from '@/api/types';
import type { Target, Turn, TurnCompare } from '@/ask/thread';
import { has, t } from '@/i18n';

/**
 * Error codes of comparisons, each with its own words (compare.error.*).
 * The judge_* and no_judge codes came from judging: a conversation kept
 * before C6 may still show them.
 */
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

/**
 * Error codes of a combined answer with words of their own
 * (compare.synth.error.*); others take the compare words. The agent names
 * the writer's failures with the judge_* codes and a missing writer
 * no_judge, as when the AIs judged.
 */
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

// The agent's limits (POST /v1/runs compare, POST /v1/comparisons).
export const maxEarlier = 3;
export const maxEarlierBytes = 4096;
export const maxAnswerBytes = 65536;
export const maxQuestionBytes = 32768;

// Codes after which the screen offers a direct comparison (비교하기).
const startAfter = ['too_many_comparisons', 'internal', 'bad_compare', 'ask_folder_unavailable'];
// Codes a retry cannot change (no_judge: a snapshot kept from before C6).
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

// ---- Who writes, and what a turn sends ----

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

/**
 * writersOf gives the asked AIs (in asked order) that can write the
 * combined answer on this PC. They go as the request's judges (the name
 * from P2-3): the agent gives the earlier questions to a writer among them.
 */
export function writersOf(info: Info, asked: string[]): string[] {
  return asked.filter((agent) => (info.agents ?? []).some((a) => a.agent === agent && canWrite(a)));
}

/**
 * compares decides at send time whether a turn is compared, which since C6
 * means the moderator's combined answer (on, or off with 'Write a combined
 * answer' once the answers are in): a question to every AI turned on, two
 * or more of them, on a PC whose AIs can write. A follow-up to one AI, a
 * one-AI turn or an older agent shows each answer in full.
 */
export function compares(o: { info: Info; asked: string[]; to: Target }): boolean {
  return o.to === 'all' && o.asked.length >= 2 && canSynthesize(o.info);
}

const doneWith = (turn: Turn, agent: string) => (turn.run?.parts ?? []).some((p) => p.agent === agent && p.status === 'done');

/**
 * earlierFor gives the earlier questions every writer's company already got
 * (each writer answered them), so the moderator reads this answer in its
 * conversation: the latest three, oldest first, each cut to 4 KiB.
 */
export function earlierFor(turns: Turn[], writers: string[]): string[] {
  if (!writers.length) return [];
  return turns
    .filter((x) => writers.every((w) => doneWith(x, w)))
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
 * directRequest is the POST /v1/comparisons body for a turn (다시 비교
 * after the PC lost the comparison, or 비교하기 on a turn kept from before
 * C6 that was never compared): the answers that came, each cut to the
 * agent's 64 KiB, with the writers (judges) and earlier questions fixed
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
 * comparing: the comparison itself still runs (it waits for the answers,
 * or is being made). Unlike polling, a done or failed comparison whose
 * combined answer is still being written is not comparing; a status this
 * app does not know is comparing while the PC asks to be polled.
 */
export function comparing(s: Pick<Comparison, 'status' | 'pollAfterMs'>): boolean {
  if (s.status === 'waiting' || s.status === 'running') return true;
  if (s.status === 'done' || s.status === 'failed') return false;
  return (s.pollAfterMs ?? 0) > 0;
}

/** canRetry: a final comparison that failed as a whole for a reason a retry may fix (the PC starts it over). */
export function canRetry(s?: Comparison): boolean {
  if (!s || polling(s)) return false;
  return s.status === 'failed' && !noRetry.includes(s.error?.code ?? '');
}

// ---- What the combined answer's block shows ----

export type CompareState =
  | { kind: 'answering' }
  | { kind: 'manual' }
  | { kind: 'oneAnswer' }
  | { kind: 'comparing' }
  | { kind: 'done' }
  | { kind: 'failed'; code: string; message?: string; action: 'retry' | 'start' | null }
  | { kind: 'notFound' }
  | { kind: 'lost'; action: 'start' | 'poll' };

/**
 * compareState is the state of a turn's comparison; null for a turn
 * without one. A turn kept from before C6 that had none (two AIs, or a PC
 * where no AI judged) shows each answer in full; one that waited for
 * 비교하기 (manual) still offers it.
 */
export function compareState(turn: Turn): CompareState | null {
  const c = turn.compare;
  if (!c || c.start === 'noJudgeOff' || c.start === 'noJudgeNone') return null;
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
  if (c.key) return { kind: 'comparing' };
  // No comparison from the run: 비교하기 makes one from the answers.
  if (!c.id) return { kind: 'manual' };
  const s = c.snap;
  if (!s || s.comparisonId !== c.id || comparing(s)) return { kind: 'comparing' };
  if (s.status === 'failed') {
    if (s.error?.code === 'need_two_answers') return { kind: 'oneAnswer' };
    return { kind: 'failed', code: s.error?.code ?? 'internal', message: s.error?.message, action: canRetry(s) ? 'retry' : null };
  }
  return { kind: 'done' };
}

// ---- Words ----

/**
 * compareErrorText explains a comparison error: its compare words, else the
 * ask words, else the general ones, else the code. An AI's own message
 * follows judge_failed on the next line.
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
  | { kind: 'done'; text: string; agent: string; model?: string; ms?: number }
  | { kind: 'failed'; code: string; message?: string; retry: boolean }
  | { kind: 'off'; cost: 1 };

// The turn's latest snapshot when it is of the turn's comparison.
const snapOf = (turn: Turn) => {
  const c = turn.compare;
  return c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
};

/**
 * synthState is the combined answer the block shows; null shows none: no
 * snapshot of this comparison, a snapshot without synthesis (an older
 * agent), an error on the turn, a comparison that is not comparing, done
 * or failed, a status this app does not know, or too few answers. Off
 * offers 'Write a combined answer' only once the comparison is final, on
 * a PC that can write. A failed one may be written again once the
 * comparison is final (else the PC says 409).
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
      const out: SynthState = { kind: 'done', text: y.text ?? '', agent: y.agent ?? '' };
      if (y.model) out.model = y.model;
      if (y.ms !== undefined) out.ms = y.ms;
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
