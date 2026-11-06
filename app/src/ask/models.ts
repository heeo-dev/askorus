// Which model and effort an AI answers with (P2-2; efforts and the own
// pick 11-06, the person: "모델 목록에 생각 수준까지", OpenCode's Flash at
// high and at max read as one model). By tier is the default; the person
// may instead pick one of the tiers' model-and-effort pairs, or pick a
// model of the tiers and any effort the PC lists for it. Everything here
// is pure: the model sheet draws it and thread.ts sends it.

import { tiers, type Tier } from '@/api/types';

import { displayModel } from './present';

/** A model and the effort it runs with ('' or absent: the model's own default). */
export type ModelPick = { model: string; effort?: string };

/** An AI's tier models as the PC reports them (/v1/info ask.tiers). */
export type TierModels = Record<string, ModelPick>;

/** The efforts the PC lists for each tier model (/v1/info ask.efforts). */
export type ModelEfforts = Record<string, string[]>;

/** The model without an OpenCode variant ('x#high' → 'x'). */
const base = (model: string) => model.split('#')[0];

/** effortOf: the effort a pick runs with: its own, else the variant its model names ('x#high'). */
export function effortOf(p: ModelPick): string {
  if (p.effort) return p.effort;
  const i = p.model.indexOf('#');
  return i >= 0 ? p.model.slice(i + 1) : '';
}

/** samePick: the two run the same model with the same effort. */
export function samePick(a: ModelPick, b: ModelPick): boolean {
  return base(a.model) === base(b.model) && effortOf(a) === effortOf(b);
}

/** modelLabel: 'haiku · medium', or the model alone when it has no effort. */
export function modelLabel(p: ModelPick): string {
  const m = displayModel(p.model);
  const e = effortOf(p);
  return e ? `${m} · ${e}` : m;
}

/** modelSpoken: 'haiku, medium', for screen readers. */
export function modelSpoken(p: ModelPick): string {
  const m = displayModel(p.model);
  const e = effortOf(p);
  return e ? `${m}, ${e}` : m;
}

/** presets: each different model and effort the tiers use, in tier order, with the tiers that use it. */
export function presets(t: TierModels): { pick: ModelPick; tiers: Tier[] }[] {
  const out: { pick: ModelPick; tiers: Tier[] }[] = [];
  for (const x of tiers) {
    const m = t[x];
    if (!m?.model) continue;
    const pick: ModelPick = m.effort ? { model: m.model, effort: m.effort } : { model: m.model };
    const same = out.find((o) => samePick(o.pick, pick));
    if (same) same.tiers.push(x);
    else out.push({ pick, tiers: [x] });
  }
  return out;
}

/** pickModels: the tiers' models, once each, in tier order: the models an own pick may use. */
export function pickModels(t: TierModels): string[] {
  const out: string[] = [];
  for (const x of tiers) {
    const m = t[x]?.model;
    if (m && !out.includes(m)) out.push(m);
  }
  return out;
}

const rank: Record<string, number> = { none: 0, minimal: 1, low: 2, medium: 3, high: 4, xhigh: 5, max: 6, ultra: 7 };

/** effortChoices: the efforts the PC lists for a model, else those its tiers run it with, least to most. */
export function effortChoices(t: TierModels, efforts: ModelEfforts | undefined, model: string): string[] {
  const listed = efforts?.[model];
  if (listed?.length) return listed;
  const used: string[] = [];
  for (const x of tiers) {
    const m = t[x];
    const e = m && m.model === model ? effortOf(m) : '';
    if (e && !used.includes(e)) used.push(e);
  }
  return used.sort((a, b) => (rank[a] ?? 99) - (rank[b] ?? 99));
}

/**
 * withModel moves a pick to another model: its effort stays when that
 * model takes it, else the effort the model's first tier runs it with,
 * else the model's first effort.
 */
export function withModel(t: TierModels, efforts: ModelEfforts | undefined, cur: ModelPick | null, model: string): ModelPick {
  const choices = effortChoices(t, efforts, model);
  const keep = cur ? effortOf(cur) : '';
  if (keep && choices.includes(keep)) return { model, effort: keep };
  const tier = tiers.map((x) => t[x]).find((m) => m?.model === model);
  const e = (tier && effortOf(tier)) || choices[0];
  return e ? { model, effort: e } : { model };
}

/** withEffort sets a pick's effort, on its model (or the given one when there is no pick yet). */
export function withEffort(cur: ModelPick | null, model: string, effort: string): ModelPick {
  return { model: cur?.model ?? model, effort };
}

/** pickOf: the person's own pick for an AI, or null when it follows the tier. */
export function pickOf(p: { models: Record<string, string>; efforts?: Record<string, string> }, agent: string): ModelPick | null {
  const model = p.models[agent];
  if (!model) return null;
  const effort = p.efforts?.[agent];
  return effort ? { model, effort } : { model };
}

/** runsWith: what an AI answers with at a tier: the person's pick, else the tier's. */
export function runsWith(pick: ModelPick | null, t: TierModels, tier: Tier): ModelPick | null {
  if (pick) return pick;
  const m = t[tier];
  return m?.model ? m : null;
}
