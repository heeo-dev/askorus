// One ask conversation on the phone (P2-2): each turn is a question and the
// answers of the AIs it went to. Follow-ups carry each AI's own session on
// (follow-up method A). Kept in memory for now; the phone history is P2-6.
//
// P2-3: a turn to every AI turned on may also compare the answers, which
// since C6 (#19) means the moderator writes them into one combined answer
// (S5b). Whether it does, who may write it, which earlier questions go
// along and whether it is written at once are fixed when it is sent
// (newTurn), so 다시 비교 later sends the same. followRun and
// followCompare follow the PC and patch the turn; the screen only draws.

import { type AgentClient, AgentError, CancelledError } from '@/api/client';
import type { Comparison, Debate, Info, Run, RunCompare, RunPart, RunRequest, RunTarget, SynthesisRequest, Tier } from '@/api/types';

import {
  compares,
  directRequest,
  earlierFor,
  errorCode,
  polling,
  refineRequest,
  synthRequest,
  withError,
  withRun,
  withSnap,
  writersOf,
} from './compare';
import { debateRequest, type TurnDebate } from './debate';

/**
 * A turn's comparison. start, judges, earlier and synth are fixed at send
 * time: start is 'auto' (compared once the answers are in), judges the
 * asked AIs that may write the combined answer, synth the writer asked for
 * or off. Conversations kept before C6 may also hold 'manual' (compared
 * only on 비교하기) and 'noJudgeOff' or 'noJudgeNone' (two AIs, never
 * compared), and turns with no synth (a PC that did not write). key is the
 * clientComparisonId of a POST /v1/comparisons not answered yet (cleared on
 * its 202); error is a code, its words chosen when drawn.
 */
export type TurnCompare = {
  start: 'auto' | 'manual' | 'noJudgeOff' | 'noJudgeNone';
  judges: string[];
  earlier: string[];
  synth?: SynthesisRequest;
  id?: string;
  key?: string;
  snap?: Comparison;
  error?: string;
};

export type Turn = {
  id: string; // also the clientRunId, so a retry never asks twice
  question: string;
  req: RunRequest;
  agents: string[]; // asked, in order
  run?: Run; // the latest state from the PC
  error?: string; // why the run could not be sent or followed (words to show)
  at?: number; // when it was sent (phone clock, ms)
  compare?: TurnCompare; // set when the turn has a Summary tab (P2-3)
  // Not sent yet: it waits until the conversation left running before it
  // is done (#23), then goes as it is.
  queued?: boolean;
  // The debate on its combined answer (토론, P2-5), kept with the turn.
  debate?: TurnDebate;
};

/** Who a follow-up goes to: everyone turned on, or one AI. */
export type Target = 'all' | { agent: string };

/** An AI the PC reports, and whether it can answer now. */
export type Asker = { agent: string; canAsk: boolean; tiers: Record<string, { model: string; effort?: string }> };

/** askers lists the PC's AIs in its order; those without the ask capability are shown dimmed. */
export function askers(info: Info): Asker[] {
  return info.agents.map((a) => ({
    agent: a.agent,
    canAsk: a.available && a.capabilities.includes('ask'),
    tiers: a.ask?.tiers ?? {},
  }));
}

/** lastSessions gives each AI's latest conversation in the thread. */
export function lastSessions(turns: Turn[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const t of turns) for (const p of t.run?.parts ?? []) if (p.session) out[p.agent] = p.session;
  return out;
}

/**
 * request builds the next run: to everyone turned on (or one AI), each
 * carrying on its own session when the thread has one, with the model the
 * person picked for that AI.
 */
export function request(opts: {
  question: string;
  tier: Tier;
  to: Target;
  on: string[];
  turns: Turn[];
  models: Record<string, string>;
  id: string;
  compare?: RunCompare;
}): RunRequest {
  const sessions = lastSessions(opts.turns);
  const names = opts.to === 'all' ? opts.on : [opts.to.agent];
  const agents: RunTarget[] = names.map((agent) => {
    const t: RunTarget = { agent };
    if (opts.models[agent]) t.model = opts.models[agent];
    if (sessions[agent]) t.session = sessions[agent];
    return t;
  });
  const req: RunRequest = { mode: 'ask', prompt: opts.question, tier: opts.tier, agents, clientRunId: opts.id };
  if (opts.compare) req.compare = opts.compare;
  return req;
}

/**
 * newTurn makes the next turn: its run, and whether it compares (compares:
 * two or more AIs on a PC that can write). A turn that compares asks the PC
 * to once the answers are in, with the combined-answer setting (synth: on,
 * and the writer), as {agent} or off.
 */
export function newTurn(o: {
  id: string;
  question: string;
  tier: Tier;
  to: Target;
  on: string[];
  turns: Turn[];
  models: Record<string, string>;
  info: Info;
  synth: { on: boolean; agent: string };
  /** Refine the question first (milestone C): the setting and the moderator; left out is off. */
  refine?: { on: boolean; agent: string };
  at: number;
}): Turn {
  const req = request({ question: o.question, tier: o.tier, to: o.to, on: o.on, turns: o.turns, models: o.models, id: o.id });
  const asked = (req.agents ?? []).map((a) => a.agent);
  const turn: Turn = { id: o.id, question: o.question, req, agents: asked, at: o.at };
  if (compares({ info: o.info, asked, to: o.to })) {
    const judges = writersOf(o.info, asked);
    const earlier = earlierFor(o.turns, judges);
    const synth = synthRequest(o.info, o.synth);
    turn.compare = { start: 'auto', judges, earlier, synth };
    req.compare = { earlier, judges, synthesis: synth };
  }
  const refine = o.refine ? refineRequest(o.info, o.refine) : undefined;
  if (refine) req.refine = refine;
  return turn;
}

/** parts shows one card per asked AI, running until the PC says otherwise. */
export function parts(turn: Turn): RunPart[] {
  return turn.agents.map(
    (agent) => turn.run?.parts.find((p) => p.agent === agent) ?? { agent, status: turn.error ? 'failed' : 'running' },
  );
}

/** running reports whether the turn still waits for an answer. */
export function running(turn: Turn): boolean {
  return !turn.error && (!turn.run || turn.run.status === 'running');
}

/**
 * What following needs from the screen: the open follows (aborted by a new
 * one for the same turn, or by 새 질문), a way to patch a turn, new ids,
 * and the words for a run that could not be followed.
 */
export type FollowDeps = {
  follows: Map<string, AbortController>;
  update: (id: string, patch: (t: Turn) => Partial<Turn>) => void;
  newId: () => string;
  errorText: (e: unknown) => string;
};

/**
 * followRun sends (or, with a run id, picks up) a turn's run and patches in
 * its answers as they come. A turn that compares then follows the
 * comparison the PC made of them.
 */
export async function followRun(client: AgentClient, turn: Turn, deps: FollowDeps, runId?: string): Promise<void> {
  deps.follows.get(turn.id)?.abort();
  const ctl = new AbortController();
  deps.follows.set(turn.id, ctl);
  // A follow that was replaced or stopped patches nothing more.
  const put = (run: Run) => {
    if (!ctl.signal.aborted) deps.update(turn.id, (t) => ({ run, compare: withRun(t.compare, run) }));
  };
  deps.update(turn.id, () => ({ error: undefined }));
  let run: Run;
  try {
    run = runId ? await client.run(runId, ctl.signal) : await client.startRun(turn.req, ctl.signal);
    put(run);
    run = await client.waitRun(run, { signal: ctl.signal, onUpdate: put });
    put(run);
  } catch (e) {
    if (!(e instanceof CancelledError) && !ctl.signal.aborted) deps.update(turn.id, () => ({ error: deps.errorText(e) }));
    return;
  } finally {
    if (deps.follows.get(turn.id) === ctl) deps.follows.delete(turn.id);
  }
  if (ctl.signal.aborted) return;
  const compare = withRun(turn.compare, run);
  if (compare?.start === 'auto' && compare.id) await followCompare(client, { ...turn, run, compare }, 'poll', deps);
}

/**
 * followCompare follows a turn's comparison until it is final: 'poll' asks
 * for the one the turn has, 'start' sends the answers for a new one (the
 * same clientComparisonId again after a lost request), 'retry' asks the PC
 * to start a comparison that failed over, 'synth' asks it to write the
 * combined answer (S5b; opts.agent the writer, left out the PC picks).
 * Errors become a code on the turn.
 */
export async function followCompare(
  client: AgentClient,
  turn: Turn,
  how: 'poll' | 'start' | 'retry' | 'synth',
  deps: FollowDeps,
  opts: { agent?: string } = {},
): Promise<void> {
  const key = `${turn.id}/compare`;
  // A second 비교하기 before the screen redraws is the same press: the first
  // POST is already out. Every follow that ended (also a lost or refused
  // start) has left the map, so a real 'start' after it still goes.
  if (how === 'start' && deps.follows.has(key)) return;
  deps.follows.get(key)?.abort();
  const ctl = new AbortController();
  deps.follows.set(key, ctl);
  const patch = (f: (c: TurnCompare) => TurnCompare) => {
    if (!ctl.signal.aborted) deps.update(turn.id, (t) => (t.compare ? { compare: f(t.compare) } : {}));
  };
  let id = turn.compare?.id;
  try {
    if (how === 'start') {
      const k = turn.compare?.key ?? deps.newId();
      const req = directRequest(turn, k);
      if (!req) return;
      // The old snapshot (of a comparison the PC lost) goes until the new one comes.
      patch((x) => ({ ...x, key: k, error: undefined, snap: undefined }));
      const snap = await client.startComparison(req, ctl.signal);
      id = snap.comparisonId;
      patch((x) => withSnap({ ...x, key: undefined }, snap));
    } else if (how === 'retry' || how === 'synth') {
      patch((x) => ({ ...x, error: undefined }));
      if (id) {
        try {
          const snap =
            how === 'retry' ? await client.retryComparison(id, ctl.signal) : await client.writeSynthesis(id, opts.agent, ctl.signal);
          patch((x) => withSnap(x, snap));
        } catch (e) {
          // Still running (another device, or a press already on its way): just follow it.
          if (!(e instanceof AgentError && e.code === 'comparison_running')) throw e;
        }
      }
    } else {
      patch((x) => ({ ...x, error: undefined }));
    }
    if (id) await client.waitComparison(id, { signal: ctl.signal, onUpdate: (s) => patch((x) => withSnap(x, s)) });
  } catch (e) {
    // An answered POST is done with its key; only an unanswered one is sent again.
    if (!(e instanceof CancelledError)) patch((x) => withError(e instanceof AgentError ? { ...x, key: undefined } : x, e));
  } finally {
    if (deps.follows.get(key) === ctl) deps.follows.delete(key);
  }
}

/**
 * followDebate follows a turn's debate (토론, P2-5): 'start' sends a new
 * one (the same clientDebateId again after a lost request), 'poll' follows
 * the one the turn has until it has ended, 'stop' asks the PC to stop it.
 * Errors become a code on the turn's debate.
 */
export async function followDebate(
  client: AgentClient,
  turn: Turn,
  info: Info | null,
  how: 'start' | 'poll' | 'stop',
  deps: FollowDeps,
): Promise<void> {
  const key = `${turn.id}/debate`;
  if (how === 'start' && deps.follows.has(key)) return; // the same press again
  if (how === 'stop') {
    const id = turn.debate?.id;
    if (!id) return;
    try {
      const snap = await client.stopDebate(id);
      deps.follows.get(key)?.abort();
      deps.update(turn.id, (t) => (t.debate ? { debate: withDebate(t.debate, snap) } : {}));
    } catch (e) {
      if (!(e instanceof CancelledError)) deps.update(turn.id, (t) => (t.debate ? { debate: { ...t.debate, error: errorCode(e) } } : {}));
    }
    return;
  }
  deps.follows.get(key)?.abort();
  const ctl = new AbortController();
  deps.follows.set(key, ctl);
  const patch = (f: (d: TurnDebate) => TurnDebate) => {
    if (!ctl.signal.aborted) deps.update(turn.id, (t) => ({ debate: f(t.debate ?? {}) }));
  };
  let id = how === 'poll' ? turn.debate?.id : undefined;
  try {
    if (how === 'start') {
      // A new debate replaces one that ended in a way it may fix; a lost request goes again with its key.
      const k = turn.debate?.key ?? deps.newId();
      const req = debateRequest(turn, info, k);
      if (!req) return;
      patch(() => ({ key: k }));
      const snap = await client.startDebate(req, ctl.signal);
      id = snap.debateId;
      patch((d) => withDebate({ ...d, key: undefined }, snap));
    } else {
      patch((d) => ({ ...d, error: undefined }));
    }
    if (id) await client.waitDebate(id, { signal: ctl.signal, onUpdate: (s) => patch((d) => withDebate(d, s)) });
  } catch (e) {
    // An answered POST is done with its key; only an unanswered one is sent again.
    if (!(e instanceof CancelledError)) patch((d) => ({ ...(e instanceof AgentError ? { ...d, key: undefined } : d), error: errorCode(e) }));
  } finally {
    if (deps.follows.get(key) === ctl) deps.follows.delete(key);
  }
}

/** withDebate keeps the latest snapshot; an answer from the PC clears an earlier error. */
export function withDebate(d: TurnDebate, s: Debate): TurnDebate {
  return { ...d, id: s.debateId, snap: s, error: undefined };
}

/**
 * stopAll stops following everything (새 질문) and asks the PC to stop the
 * comparisons that are not final (also one whose combined answer is still
 * being written) and the debates that run, without waiting for it.
 */
export function stopAll(client: AgentClient | undefined, turns: Turn[], follows: Map<string, AbortController>): void {
  follows.forEach((c) => c.abort());
  follows.clear();
  stopComparisons(client, turns);
}

/**
 * stopTurns stops following these turns only (a removed conversation) and
 * asks the PC to stop their comparisons that are not final; another
 * conversation still followed in the background (#23) goes on.
 */
export function stopTurns(client: AgentClient | undefined, turns: Turn[], follows: Map<string, AbortController>): void {
  for (const t of turns) {
    for (const key of [t.id, `${t.id}/compare`, `${t.id}/debate`]) {
      follows.get(key)?.abort();
      follows.delete(key);
    }
  }
  stopComparisons(client, turns);
}

function stopComparisons(client: AgentClient | undefined, turns: Turn[]): void {
  if (!client) return;
  for (const t of turns) {
    // A debate that still runs stops too (its turn's conversation is gone).
    const d = t.debate;
    if (d?.id && !d.error && d.snap?.debateId === d.id && d.snap.pollAfterMs) client.stopDebate(d.id).catch(() => undefined);
  }
  for (const t of turns) {
    const c = t.compare;
    if (!c?.id || c.error === 'comparison_not_found') continue;
    if (c.snap && c.snap.comparisonId === c.id && !polling(c.snap)) continue;
    client.cancelComparison(c.id).catch(() => undefined);
  }
}
