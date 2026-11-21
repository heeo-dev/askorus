// The pictures AIs make in their answers (#14, made pictures, decisions of
// 2026-11-21). A run's part names each picture by the PC's id; the app takes
// each into the conversation's folder (madeFiles.ts) as soon as the part
// says so, shows it under that AI's answer and in the row of pictures, and
// keeps it with the conversation. The text never waits for a picture. A
// picture that could not be taken shows 'Try again'; the PC keeps its copy
// a day.

import { type AgentClient } from '@/api/client';
import type { MadeImage, Run } from '@/api/types';

import { checkMade, madeTarget } from './madeFiles';

/** What a turn keeps of its made pictures: where each one taken lies, and the ids that could not be taken. */
export type TurnMade = { made?: Record<string, string>; madeFailed?: string[] };

/** One made picture as a screen shows it: its turn, where it is kept, or that it could not be taken (yet). */
export type MadeShown = { turnId: string; runId: string; image: MadeImage; uri?: string; failed?: boolean };

/** At most this many pictures of one answer are shown (the PC sends at most 4). */
export const maxMadeShown = 4;

/** madeOf lists the pictures of agent's answer in a turn, as the screens show them. */
export function madeOf(turn: TurnMade & { id: string; run?: Run }, agent: string): MadeShown[] {
  const run = turn.run;
  const part = run?.parts.find((p) => p.agent === agent);
  if (!run || !part?.images?.length) return [];
  return part.images.slice(0, maxMadeShown).map((image) => ({
    turnId: turn.id,
    runId: run.runId,
    image,
    uri: turn.made?.[image.id],
    failed: turn.madeFailed?.includes(image.id) || undefined,
  }));
}

/** madeWanted lists the pictures of a run that are neither kept nor failed. */
export function madeWanted(run: Run, kept: TurnMade): MadeImage[] {
  const out: MadeImage[] = [];
  for (const p of run.parts) {
    for (const im of (p.images ?? []).slice(0, maxMadeShown)) {
      if (!kept.made?.[im.id] && !kept.madeFailed?.includes(im.id)) out.push(im);
    }
  }
  return out;
}

/**
 * takeMade takes one picture of a run into the conversation's folder: its
 * address, or null when it could not be taken or is not the picture the PC
 * named (its size and first bytes are checked again; a wrong file is
 * deleted).
 */
export async function takeMade(client: AgentClient, conversationId: string, runId: string, image: MadeImage, signal?: AbortSignal): Promise<string | null> {
  const uri = madeTarget(conversationId, image);
  if (!uri) return null;
  try {
    await client.runImage(runId, image.id, uri, signal);
  } catch {
    checkMade(uri, { ...image, bytes: -1 }); // deletes anything half written
    return null;
  }
  return checkMade(uri, image) ? uri : null;
}
