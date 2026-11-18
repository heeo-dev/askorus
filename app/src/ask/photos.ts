// Photos with a question (#14). The person adds photos to a question (the
// camera, or the phone's photo picker); the app scales each down and keeps
// its own copy with the conversation; when the question goes, the copies
// are sent to the PC first and the question names them. This file is the
// part without a screen or a native module: what a PC takes, which AIs see
// photos, and what is said when something goes wrong.

import type { Info, Tier } from '@/api/types';

/**
 * One photo of a question as the phone keeps it: the app's own scaled-down
 * copy (a file:// address in the app's files) and its size on the screen.
 */
export type TurnPhoto = { uri: string; width: number; height: number };

/** The long edge a photo is scaled down to, and its JPEG quality, before it is sent. */
export const photoEdge = 2048;
export const photoQuality = 0.8;

/** photoLimit is how many photos a question may carry on this PC: 0 when the PC takes none (an older agent). */
export function photoLimit(info: Info | null | undefined): number {
  const n = info?.attachments?.maxCount;
  return typeof n === 'number' && n > 0 ? Math.floor(n) : 0;
}

/** modelBase is a model's name without its '#variant' (OpenCode). */
const modelBase = (model: string) => model.split('#')[0];

/**
 * seesPhotos tells whether an AI, answering with model, sees photos: the PC
 * says so per AI (ask.images: all of its models, or the ones listed). An AI
 * the PC says nothing about takes none.
 */
export function seesPhotos(info: Info, agent: string, model: string | undefined): boolean {
  const images = info.agents.find((a) => a.agent === agent)?.ask?.images;
  if (!images) return false;
  if (images.all) return true;
  return !!model && (images.models ?? []).includes(modelBase(model));
}

/**
 * askModel is the model an AI answers a question with: the one the person
 * picked for it, else its model of the tier.
 */
export function askModel(info: Info, agent: string, tier: Tier, picked: Record<string, string>): string | undefined {
  return picked[agent] || info.agents.find((a) => a.agent === agent)?.ask?.tiers?.[tier]?.model;
}

/**
 * byPhotos splits the AIs a question with photos would go to: those that
 * see photos, and those that do not (left out of that question, and named
 * to the person before it goes). carried are the AIs whose conversation the
 * question carries on: an AI that sees photos only in a new conversation
 * (images.newOnly, Antigravity, R-130) does not see them there.
 */
export function byPhotos(info: Info, agents: string[], tier: Tier, picked: Record<string, string>, carried: Record<string, string> = {}): { sees: string[]; blind: string[] } {
  const sees: string[] = [];
  const blind: string[] = [];
  for (const agent of agents) {
    const newOnly = !!info.agents.find((a) => a.agent === agent)?.ask?.images?.newOnly;
    const sees1 = seesPhotos(info, agent, askModel(info, agent, tier, picked)) && !(newOnly && carried[agent]);
    (sees1 ? sees : blind).push(agent);
  }
  return { sees, blind };
}

/** scaled gives the size a photo of w by h is scaled down to: its long edge at most photoEdge, never up. */
export function scaled(width: number, height: number, edge = photoEdge): { width: number; height: number } {
  const long = Math.max(width, height);
  if (!(long > edge)) return { width: Math.round(width), height: Math.round(height) };
  const k = edge / long;
  return { width: Math.max(1, Math.round(width * k)), height: Math.max(1, Math.round(height * k)) };
}

/** The agent's error codes for photos; each has words (ask.photo.error.<code>). */
export const photoCodes = ['uploads_unavailable', 'upload_too_large', 'upload_bad_type', 'upload_limit', 'upload_gone', 'no_image_support'] as const;
