// What the 한 번에 (Ask) screen shows of a turn (S5c), as plain values the
// screen draws: the AIs' names, models and times, the combined answer's
// title and step, the AI rows and cards, and answers in plain text.
// compare.ts and thread.ts decide the states; this only words and orders
// them, so it is tested without a screen. (The judging's headline,
// evidence and details went with C6d, #24.)
//
// Fixes from the S5b phone check (docs/phase2-plan.md part 23):
// - R-39 a model shows without its variant ('#high') and provider
//   ('opencode-go/') everywhere.
// - R-40 every accessibility value is an object (rowValue).

import { agentName } from '@/api/agents';
import { AgentError } from '@/api/client';
import type { Comparison, Info, RunPart } from '@/api/types';
import { compareState, synthState } from '@/ask/compare';
import { parts, type Target, type Turn } from '@/ask/thread';
import { errorText, has, type MessageKey, t } from '@/i18n';
import type { StepView } from '@/ui/CombinedAnswer';
import { inlineMathSpans, texOfSpan } from '@/ui/inlineMath';
import { texToUnicode } from '@/ui/tex';

// ---- Errors ----

/** partError explains why an AI gave no answer; the AI's own message follows. */
export function partError(e: { code: string; message?: string }): string {
  const askKey = `ask.error.${e.code}`;
  const key = `error.${e.code}`;
  const base = has(askKey)
    ? t(askKey as MessageKey, { folder: e.message ?? '' })
    : has(key)
      ? t(key as MessageKey)
      : t('error.unknown', { code: e.code });
  const own = e.code === 'ask_failed' || e.code === 'bad_model' ? e.message : '';
  return own ? `${base}\n${own}` : base;
}

/** askErrorText: the PC's error codes in ask words where an ask needs other words (the ask folder, a model). */
export function askErrorText(e: unknown): string {
  return e instanceof AgentError ? partError({ code: e.code, message: e.message }) : errorText(e);
}

const firstLine = (s: string) => s.split('\n')[0] ?? '';

// ---- Names, models and times ----

/**
 * displayModel is a model as people read it (R-39): without the variant
 * after '#' and the provider before the last '/', so
 * 'opencode-go/deepseek-v4.1-flash#high' shows as 'deepseek-v4.1-flash'.
 * A name that would come out empty shows as given.
 */
export function displayModel(model?: string | null): string {
  const given = (model ?? '').trim();
  let m = given;
  const hash = m.indexOf('#');
  if (hash >= 0) m = m.slice(0, hash);
  const slash = m.lastIndexOf('/');
  if (slash >= 0) m = m.slice(slash + 1);
  return m.trim() || given;
}

/** whole seconds of a time in ms, for lists ('9 s'). */
export const wholeSeconds = (ms: number) => Math.max(0, Math.round(ms / 1000));

/** seconds with tenths, for one answer's time ('9.8 s'). */
export const tenths = (ms: number) => t('ask.seconds', { s: (ms / 1000).toFixed(1) });

// ---- The AIs of a turn ----

/**
 * PartView is one asked AI as the screen shows it. s is whole seconds: how
 * long its answer took, or, still answering, how long since the question
 * went. error is why it gave no answer.
 */
export type PartView = {
  agent: string;
  name: string;
  model: string;
  state: 'answering' | 'done' | 'failed';
  s: number;
  part: RunPart;
  error?: string;
};

/**
 * askedModel is the model a turn asked an AI for: the one picked for it
 * in the AI sheet, else its tier's model on the PC (when its info is
 * known), else none.
 */
export function askedModel(turn: Turn, agent: string, info?: Info | null): string {
  const picked = turn.req.agents?.find((a) => a.agent === agent)?.model;
  if (picked) return picked;
  const tier = turn.req.tier;
  return (tier && info?.agents.find((a) => a.agent === agent)?.ask?.tiers[tier]?.model) || '';
}

/**
 * partViews lists the asked AIs in asked order, with how each answer
 * stands. The model is the one the PC says answered; until it says, the
 * one the turn asked for (UX-8), so the rows do not change shape when
 * the answers come.
 */
export function partViews(turn: Turn, now: number, info?: Info | null): PartView[] {
  const since = turn.at !== undefined ? wholeSeconds(now - turn.at) : 0;
  return parts(turn).map((p) => {
    const state: PartView['state'] = p.status === 'done' ? 'done' : p.status === 'running' && !turn.error ? 'answering' : 'failed';
    const v: PartView = {
      agent: p.agent,
      name: agentName(p.agent),
      model: displayModel(p.model || askedModel(turn, p.agent, info)),
      state,
      s: state !== 'answering' && p.ms !== undefined ? wholeSeconds(p.ms) : since,
      part: p,
    };
    // No answer and no reason from the PC (the run could not be followed): the turn's error says why.
    if (state === 'failed') v.error = p.error ? partError(p.error) : t('compare.tab.failed');
    return v;
  });
}

/** rowValue is what a screen reader hears as an AI row's state; always an object (R-40). */
export function rowValue(p: Pick<PartView, 'state' | 's'>): { text: string } {
  if (p.state === 'done') return { text: t('ask.rowDone', { s: p.s }) };
  if (p.state === 'answering') return { text: t('ask.rowAnswering', { s: p.s }) };
  return { text: t('compare.tab.failed') };
}

/** answeredAgents: the AIs (codes) that answered, in asked order. */
export function answeredAgents(turn: Turn): string[] {
  return parts(turn)
    .filter((p) => p.status === 'done')
    .map((p) => p.agent);
}

/** The turn's latest snapshot when it is of the turn's comparison. */
export function snapshotOf(turn: Turn): Comparison | undefined {
  const c = turn.compare;
  return c?.snap && c.snap.comparisonId === c.id ? c.snap : undefined;
}

/**
 * turnTimes: how long the answers took (the slowest AI that ended) and,
 * once it is written, how long the combined answer took.
 */
export function turnTimes(turn: Turn): { answersMs?: number; synthMs?: number } {
  const out: { answersMs?: number; synthMs?: number } = {};
  const answers = (turn.run?.parts ?? [])
    .filter((p) => turn.agents.includes(p.agent) && p.status !== 'running' && typeof p.ms === 'number')
    .map((p) => p.ms as number);
  if (answers.length) out.answersMs = Math.max(...answers);
  const y = snapshotOf(turn)?.synthesis;
  if (y?.status === 'done' && typeof y.ms === 'number') out.synthMs = y.ms;
  return out;
}

/** metaLine: 'answers 12 s · combined in 15 s', each part when known. */
export function metaLine(turn: Turn): string {
  const { answersMs, synthMs } = turnTimes(turn);
  return [
    answersMs !== undefined ? t('ask.metaAnswers', { s: wholeSeconds(answersMs) }) : '',
    synthMs !== undefined ? t('ask.metaSynth', { s: wholeSeconds(synthMs) }) : '',
  ]
    .filter(Boolean)
    .join(' · ');
}

// ---- The combined answer's block ----

/**
 * synthExpected: a combined answer is still to come. From the snapshot
 * when there is one (waiting or being written); before it, what the turn
 * asked for when it was sent.
 */
export function synthExpected(turn: Turn): boolean {
  const s = snapshotOf(turn);
  if (s) return !!s.synthesis && (s.synthesis.status === 'waiting' || s.synthesis.status === 'running');
  return !!turn.compare?.synth && !turn.compare.synth.off;
}

/**
 * blockTitle: 종합 답변 while the comparison runs or once it did, when one
 * is coming or came (also failed); else 비교 (also before 비교하기, after a
 * lost or forgotten comparison, with one answer, and while it is off).
 */
export function blockTitle(turn: Turn, info: Info | null): string {
  const kind = compareState(turn)?.kind;
  if (kind !== 'comparing' && kind !== 'done' && kind !== 'failed') return t('compare.title');
  const y = synthState(turn, info);
  const synth = synthExpected(turn) || y?.kind === 'waiting' || y?.kind === 'writing' || y?.kind === 'done' || y?.kind === 'failed';
  return synth ? t('compare.synth.title') : t('compare.title');
}

/**
 * steps are the steps of making the combined answer (F-Ask-Comparing):
 * since C6 only one, 'Writing the combined answer…', while one is coming:
 * as the comparison starts, or once it is final and the answer waits to be
 * written or is being written. Otherwise none.
 */
export function steps(turn: Turn, info: Info | null): StepView[] {
  const st = compareState(turn);
  if (!st) return [];
  const y = synthState(turn, info);
  const coming = st.kind === 'comparing' ? synthExpected(turn) : (st.kind === 'done' || st.kind === 'failed') && (y?.kind === 'waiting' || y?.kind === 'writing');
  return coming ? [{ key: 'synth', label: t('compare.step.synth') }] : [];
}

// ---- The AI sheet and the AI cards ----

/** aiStatus: an AI's state at the right of its row in the AI sheet (B2-Many-Sheet). */
export function aiStatus(p: PartView): { tone: 'sub' | 'waiting' | 'danger'; text: string; icon?: 'check' | 'spinner' } {
  if (p.state === 'done') return { tone: 'sub', icon: 'check', text: t('ask.seconds', { s: p.s }) };
  if (p.state === 'answering') return { tone: 'waiting', icon: 'spinner', text: t('ask.answering') };
  return { tone: 'danger', text: t('ask.failedReason', { reason: firstLine(p.error ?? '') }) };
}

/** cardPreview: the start of an AI's answer in plain text, or how it stands. */
export function cardPreview(p: PartView): string {
  if (p.state === 'done') return plainPreview(p.part.answer ?? '');
  if (p.state === 'answering') return t('ask.answering');
  return firstLine(p.error ?? '');
}

/** onlyTarget: the AI a follow-up goes to alone, or null for several. */
export const onlyTarget = (to: Target): string | null => {
  if (to === 'all') return null;
  if ('agent' in to) return to.agent;
  return to.agents.length === 1 ? to.agents[0] : null;
};

// ---- Plain text of Markdown ----

const entities: Record<string, string> = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", '#39': "'", nbsp: ' ' };

/**
 * plainPreview is the start of an answer as plain text, for a card that
 * shows two lines of it: the Markdown marks taken out (headings, lists,
 * quotes, emphasis, links, code marks, tables, HTML), inline math as
 * Unicode where it can be, white space folded, cut to max characters with
 * '…'. Code inside backticks stays as written (never read as math).
 */
export function plainPreview(markdown: string, max = 160): string {
  let s = markdown.slice(0, Math.max(max * 8, 2000)).replace(/\r\n?/g, '\n');
  s = s
    .replace(/^[ \t]*(```|~~~).*$/gm, ' ')
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/<\/?[A-Za-z][^<>]*>/g, ' ')
    .replace(/^[ \t]*\|?[ \t]*:?-{3,}:?[ \t]*(\|[ \t]*:?-{3,}:?[ \t]*)*\|?[ \t]*$/gm, ' ')
    .replace(/^[ \t]*([-*_][ \t]*){3,}$/gm, ' ')
    .replace(/^[ \t]{0,3}#{1,6}[ \t]+/gm, '')
    .replace(/^[ \t]*(>[ \t]?)+/gm, '')
    .replace(/^[ \t]*([-*+]|\d{1,9}[.)])[ \t]+(\[[ xX]\][ \t]+)?/gm, '')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[\^[^\]]+\]/g, '')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\[[^\]]*\]/g, '$1');
  // Code spans stay as written; the rest loses emphasis marks and its math becomes text.
  let out = '';
  const code = /(`+)([\s\S]*?)\1/g;
  let at = 0;
  for (let m = code.exec(s); m; m = code.exec(s)) {
    out += prose(s.slice(at, m.index)) + m[2];
    at = m.index + m[0].length;
  }
  out += prose(s.slice(at));
  out = out
    .replace(/\|/g, ' ')
    .replace(/&(amp|lt|gt|quot|apos|#39|nbsp);/g, (_m, e: string) => entities[e] ?? _m)
    .replace(/\s+/g, ' ')
    .trim();
  return cutText(out, max);
}

function prose(seg: string): string {
  let out = '';
  let at = 0;
  for (const sp of inlineMathSpans(seg)) {
    out += marksOff(seg.slice(at, sp.start));
    const raw = seg.slice(sp.start, sp.end);
    const tex = texOfSpan(raw);
    out += tex ? (texToUnicode(tex.tex) ?? tex.tex) : raw;
    at = sp.end;
  }
  return out + marksOff(seg.slice(at));
}

const marksOff = (s: string) =>
  s.replace(/(\*\*|__|~~)/g, '').replace(/(^|[^\w*])[*_](?=\S)([^*_\n]*?\S)[*_](?=[^\w*]|$)/g, '$1$2').replace(/`+/g, '');

/** cutText keeps the first max characters (never half an emoji), with '…' when cut. */
export function cutText(s: string, max: number): string {
  if (s.length <= max) return s;
  let end = Math.max(0, max - 1);
  const c = s.charCodeAt(end - 1);
  if (c >= 0xd800 && c <= 0xdbff) end--;
  return `${s.slice(0, end).trimEnd()}…`;
}
