// One ask conversation on the phone (P2-2): each turn is a question and the
// answers of the AIs it went to. Follow-ups carry each AI's own session on
// (follow-up method A). Kept in memory for now; the phone history is P2-5.

import type { Info, Run, RunPart, RunRequest, RunTarget, Tier } from '@/api/types';

export type Turn = {
  id: string; // also the clientRunId, so a retry never asks twice
  question: string;
  req: RunRequest;
  agents: string[]; // asked, in order
  run?: Run; // the latest state from the PC
  error?: string; // why the run could not be sent or followed (words to show)
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
}): RunRequest {
  const sessions = lastSessions(opts.turns);
  const names = opts.to === 'all' ? opts.on : [opts.to.agent];
  const agents: RunTarget[] = names.map((agent) => {
    const t: RunTarget = { agent };
    if (opts.models[agent]) t.model = opts.models[agent];
    if (sessions[agent]) t.session = sessions[agent];
    return t;
  });
  return { mode: 'ask', prompt: opts.question, tier: opts.tier, agents, clientRunId: opts.id };
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
