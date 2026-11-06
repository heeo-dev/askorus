// The debate on the phone (토론, P2-5, #26): when a turn offers '토론하기',
// what it sends, the state of its debate from the PC's snapshot (GET
// /v1/debates/{id}) and the words of its chips. Everything here is pure:
// thread.ts follows the debate and the screen draws.
//
// A debate starts from a turn whose combined answer is done: the answers
// that came (on this PC, so every AI speaks), the combined answer, its
// writer as the moderator, the earlier questions and the AIs they may go
// to as fixed when the turn was sent, and the question's tier. The phone
// keeps the debate with its turn (the PC forgets it 30 minutes after it
// ended).

import { agentName } from '@/api/agents';
import type { Debate, DebateRequest, Info } from '@/api/types';
import { has, t } from '@/i18n';

import { plainNames } from './aiNames';
import { compareErrorText, cutUtf8, maxAnswerBytes, maxQuestionBytes, readyParts, synthState } from './compare';
import type { Turn } from './thread';

/**
 * A turn's debate. key is the clientDebateId of a POST /v1/debates not
 * answered yet (cleared on its 202); error is a code, its words chosen when
 * drawn.
 */
export type TurnDebate = { id?: string; key?: string; snap?: Debate; error?: string };

/** The combined answer's limit, which the agent also puts on the text a debate starts from. */
export const maxCombinedBytes = 65536;

/** Debate error codes with words of their own (debate.error.*); others take the compare words. */
export const debateCodes = ['too_many_debates', 'debate_not_found', 'judge_bad_output', 'judge_timeout', 'no_judge', 'cancelled', 'bad_model'] as const;

// Codes after which the person may debate again.
const again = ['too_many_debates', 'debate_not_found', 'judge_bad_output', 'judge_timeout', 'judge_failed', 'internal', 'network', 'cancelled', 'bad_model'];

/** The combined answer a debate starts from: the turn's, once it is done. */
function combined(turn: Turn, info: Info | null): { text: string; agent: string } | null {
  const s = synthState(turn, info);
  return s?.kind === 'done' && s.text.trim() ? { text: s.text, agent: s.agent } : null;
}

/**
 * debateOffered: the turn may start a debate: the PC runs them (info's
 * debate), the combined answer is done, two or more answers came, and the
 * turn has no debate yet (or one that ended in a way a new one may fix).
 */
export function debateOffered(turn: Turn, info: Info | null): boolean {
  if (!info?.debate || turn.error || !combined(turn, info) || readyParts(turn).length < 2) return false;
  const d = turn.debate;
  if (!d) return true;
  if (d.key) return false;
  if (d.error) return again.includes(d.error);
  const s = d.snap;
  return !!s && s.debateId === d.id && s.status === 'failed' && again.includes(s.error?.code ?? 'internal');
}

/**
 * debateCost: about how many more AI calls a debate takes: the plan and
 * the answer after it, and for each of up to two points a turn per AI (up
 * to three) and a check. Three AIs: 10, two: 8.
 */
export function debateCost(turn: Turn): number {
  const speakers = Math.min(readyParts(turn).length, 3);
  return 2 + 2 * (speakers + 1);
}

/** debateRequest is the POST /v1/debates body for a turn, or null when it cannot debate. */
export function debateRequest(turn: Turn, info: Info | null, key: string): DebateRequest | null {
  const c = combined(turn, info);
  const answers = readyParts(turn).map((p) => {
    const a: DebateRequest['answers'][number] = { agent: p.agent, via: 'pc', text: cutUtf8(p.answer ?? '', maxAnswerBytes) };
    if (p.model) a.model = p.model;
    return a;
  });
  if (!c || answers.length < 2) return null;
  const req: DebateRequest = {
    clientDebateId: key,
    question: cutUtf8(turn.question, maxQuestionBytes),
    earlier: turn.compare?.earlier ?? [],
    judges: turn.compare?.judges ?? [],
    answers,
    synthesis: cutUtf8(c.text, maxCombinedBytes),
  };
  if (c.agent) req.moderator = c.agent;
  if (turn.req.tier) req.tier = turn.req.tier;
  // The moderator model the turn asked for its writer (11-06), when that AI wrote.
  const synth = turn.compare?.synth;
  if (c.agent && synth?.agent === c.agent && synth.model) {
    req.moderatorModel = synth.model;
    if (synth.effort) req.moderatorEffort = synth.effort;
  }
  // The conversation record the turn went with (#29): what earlier debates settled.
  if (turn.req.context) req.context = turn.req.context;
  return req;
}

// ---- State ----

export type DebateView =
  | { kind: 'starting' }
  | { kind: 'planning' }
  | { kind: 'running'; snap: Debate }
  | { kind: 'writing'; snap: Debate }
  | { kind: 'done'; snap: Debate }
  | { kind: 'none' }
  | { kind: 'stopped'; snap: Debate }
  | { kind: 'failed'; snap?: Debate; code: string; message?: string; again: boolean }
  | { kind: 'lost'; action: 'start' | 'poll' };

/** debateView is what the turn's debate shows; null when it has none. */
export function debateView(turn: Turn): DebateView | null {
  const d = turn.debate;
  if (!d) return null;
  const s = d.snap && d.snap.debateId === d.id ? d.snap : undefined;
  if (d.error === 'network') return { kind: 'lost', action: d.key ? 'start' : 'poll' };
  if (d.error) return { kind: 'failed', snap: s, code: d.error, again: again.includes(d.error) };
  if (d.key || !s) return { kind: 'starting' };
  switch (s.status) {
    case 'planning':
      return { kind: 'planning' };
    case 'writing':
      return { kind: 'writing', snap: s };
    case 'done':
      return s.points.some((p) => p.debate) || s.after ? { kind: 'done', snap: s } : { kind: 'none' };
    case 'stopped':
      return { kind: 'stopped', snap: s };
    case 'failed': {
      const code = s.error?.code || 'internal';
      return { kind: 'failed', snap: s, code, message: s.error?.message, again: again.includes(code) };
    }
    default:
      // 'debating', or a status this app does not know while the PC asks to be polled.
      return s.pollAfterMs ? { kind: 'running', snap: s } : { kind: 'failed', snap: s, code: 'internal', again: true };
  }
}

/** debating: the debate still runs on the PC (or its request is on its way). */
export function debating(turn: Turn): boolean {
  const v = debateView(turn);
  return !!v && (v.kind === 'starting' || v.kind === 'planning' || v.kind === 'running' || v.kind === 'writing');
}

// ---- Words ----

/** natureLabel: the point's nature chip (팩트 · 추론 · 판단 · 취향); an unknown one shows nothing. */
export function natureLabel(nature: string): string {
  const key = `debate.nature.${nature}`;
  return has(key) ? t(key) : '';
}

/** stanceLabel: a turn's stance chip (유지 · 일부 바꿈 · 바꿈); none shows nothing. */
export function stanceLabel(stance?: string): string {
  const key = `debate.stance.${stance ?? ''}`;
  return stance && has(key) ? t(key) : '';
}

/** resultLabel: a closed point's outcome chip (합의 · 상황별 · 여전히 다름 · 선택지 유지). */
export function resultLabel(result?: string): string {
  const key = `debate.result.${result ?? ''}`;
  return result && has(key) ? t(key) : '';
}

/** debateErrorText explains why a debate failed: its own words, else the comparison's. */
export function debateErrorText(code: string, message?: string): string {
  const own = `debate.error.${code}`;
  return has(own) ? t(own) : compareErrorText(code, message);
}

/** sideLine: one side of a point, "Claude, Codex: 8시". */
export function sideLine(side: { ais: string[]; says: string }): string {
  return `${side.ais.map(agentName).join(', ')}: ${side.says}`;
}

/**
 * foldText is a turn's start as plain words, for its folded bubble: AI
 * names without braces and no Markdown marks (bold, code, headings, links
 * as their words), which a folded line cannot draw (S10, 11-06:
 * '**Mapo Dalbit Bakery**' showed as typed).
 */
export function foldText(text: string): string {
  return plainNames(text)
    .replace(/\[([^\]\n]+)\]\([^)\s]+\)/g, '$1')
    .replace(/(\*\*|__|`)/g, '')
    .replace(/^#{1,6}\s+/gm, '');
}

/** duration: "2분 26초" / "2 min 26 s"; under a minute "38초" / "38 s". */
export function duration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? t('ask.seconds', { s }) : t('debate.minutes', { m: Math.floor(s / 60), s: s % 60 });
}
