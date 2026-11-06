// The conversation record (#29, 11-06): what a follow-up carries of the
// conversation before it, so the AIs build on what was concluded instead
// of starting over. The phone builds it from the turns each time a
// question goes (a question that waited, at the moment it goes), and the
// PC keeps nothing of it. It goes with the follow-up's run, and with a
// comparison made again or a debate of that turn, as fixed when the turn
// was sent.
//
// The record: the latest earlier questions (for an AI new to the
// conversation), every point an earlier debate closed (done or stopped)
// with a result, and the previous turn's final answer: the answer after
// its debate, else its combined answer, else its one AI's answer (none
// when several AIs answered and nothing combined them). Everything here is
// pure.

import type { Info, RequestContext, Settled } from '@/api/types';

import { cutUtf8, maxEarlier, maxEarlierBytes, readyParts, synthState } from './compare';
import type { Turn } from './thread';

/** The agent's limits on a record (also in /v1/info's context). */
export const maxSettled = 12;
export const maxTopicBytes = 512;
export const maxLineBytes = 1024;
export const maxLastBytes = 8192;

/** The results a settled point may have: a debate's outcomes. */
const results = ['agreed', 'conditional', 'differ', 'kept'];

/** clean drops NULs (the agent refuses them) and cuts s to maxBytes of UTF-8. */
function clean(s: string, maxBytes: number): string {
  return cutUtf8(s.replace(/\u0000/g, '').trim(), maxBytes);
}

/** sentTurns are the turns that reached the PC: a run, not waiting. */
function sentTurns(turns: Turn[]): Turn[] {
  return turns.filter((x) => !x.queued && !!x.run);
}

/** The turn's debate snapshot when it ended done or stopped. */
function endedDebate(turn: Turn) {
  const d = turn.debate;
  const s = d?.snap;
  if (!d || d.error || !s || s.debateId !== d.id) return null;
  return s.status === 'done' || s.status === 'stopped' ? s : null;
}

/** settledOf: the points the turns' debates closed with a result, oldest first, the latest maxSettled. */
export function settledOf(turns: Turn[]): Settled[] {
  const out: Settled[] = [];
  for (const turn of sentTurns(turns)) {
    for (const p of endedDebate(turn)?.points ?? []) {
      if (!p.result || !results.includes(p.result)) continue;
      const topic = clean(p.topic ?? '', maxTopicBytes);
      const line = clean(p.line ?? '', maxLineBytes);
      if (topic && line) out.push({ topic, result: p.result, line });
    }
  }
  return out.slice(-maxSettled);
}

/**
 * finalAnswer is a turn's final answer: the answer after its debate, else
 * its combined answer, else its one AI's answer; '' when several AIs
 * answered and nothing combined them.
 */
export function finalAnswer(turn: Turn): string {
  const after = endedDebate(turn)?.after?.text;
  if (after?.trim()) return after;
  const s = synthState(turn, null);
  if (s?.kind === 'done' && s.text.trim()) return s.text;
  const ready = readyParts(turn);
  return ready.length === 1 ? (ready[0].answer ?? '') : '';
}

/**
 * buildRecord is the record of a question sent after these turns, or null
 * when the PC takes none (an older agent) or there is nothing concluded to
 * carry: no settled point and no previous final answer.
 */
export function buildRecord(turns: Turn[], info: Info | null): RequestContext | null {
  const limits = info?.context;
  if (!limits) return null;
  const sent = sentTurns(turns);
  if (!sent.length) return null;
  const settled = settledOf(sent).slice(-Math.max(0, Math.min(maxSettled, limits.maxSettled)));
  const last = clean(finalAnswer(sent[sent.length - 1]), Math.min(maxLastBytes, limits.maxLastBytes));
  if (!settled.length && !last) return null;
  const out: RequestContext = {
    earlier: sent
      .slice(-maxEarlier)
      .map((x) => clean(x.question, maxEarlierBytes))
      .filter(Boolean),
  };
  if (settled.length) out.settled = settled;
  if (last) out.last = last;
  return out;
}
