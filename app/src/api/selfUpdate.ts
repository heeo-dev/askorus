// Following an agent update from the phone (#35, Phase 1 plan part 27).
// After the phone's go the agent first waits for the questions still being
// answered (up to ten minutes, running: true), then swaps its program and
// ends; the PC does not answer for a moment; then the new agent answers
// with the new version, or the old one comes back and says why. The phone
// asks every few seconds and reads which of these it is.

import type { AgentClient } from './client';
import type { UpdateStatus } from './types';

export type UpdateOutcome =
  | { kind: 'done'; version: string }
  | { kind: 'rolledBack'; reason?: string }
  | { kind: 'failed'; reason?: string }
  | { kind: 'notTaken'; reason?: string }
  /** The PC did not come back in time. */
  | { kind: 'lost' };

/** waiting: the agent waits for questions to finish; away: it is swapping and does not answer. */
export type UpdatePhase = 'waiting' | 'away';

/** The outcome a status shows for the update to version to, if it has ended. */
export function outcomeOf(st: UpdateStatus, to: string): UpdateOutcome | null {
  if (st.current === to) return { kind: 'done', version: to };
  // While running the record may still be an earlier try's.
  if (st.running || !st.last || st.last.to !== to) return null;
  switch (st.last.state) {
    case 'rolled-back':
      return { kind: 'rolledBack', reason: st.last.reason };
    case 'failed':
      return { kind: 'failed', reason: st.last.reason };
    case 'not-taken':
      return { kind: 'notTaken', reason: st.last.reason };
  }
  return null;
}

export type FollowOptions = {
  onPhase?: (p: UpdatePhase) => void;
  signal?: AbortSignal;
  /** How often to ask (3 s). */
  every?: number;
  /** How long to follow: up to ten minutes of waiting, ten of downloading and the swap (25 min, R-138). */
  limitMs?: number;
  now?: () => number;
};

/** How often the phone asks while it follows an update; tests shorten it. */
export const followTiming = { every: 3000 };

const pause = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve) => {
    const id = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => {
      clearTimeout(id);
      resolve();
    });
  });

/** followUpdate asks the PC until the update to version to has ended. */
export async function followUpdate(client: AgentClient, to: string, o: FollowOptions = {}): Promise<UpdateOutcome> {
  const every = o.every ?? followTiming.every;
  const now = o.now ?? Date.now;
  const end = now() + (o.limitMs ?? 25 * 60 * 1000);
  while (!o.signal?.aborted) {
    try {
      const st = await client.updateStatus(o.signal);
      const out = outcomeOf(st, to);
      if (out) return out;
      o.onPhase?.(st.running ? 'waiting' : 'away');
    } catch {
      // The old agent has ended and the new one is not up yet.
      o.onPhase?.('away');
    }
    if (now() >= end) break;
    await pause(every, o.signal);
  }
  return { kind: 'lost' };
}
