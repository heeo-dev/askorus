// What the 한 번에 (Ask) screen shows of a turn (S5c), as plain values the
// screen draws: the AIs' names, models and times, the steps of making the
// combined answer, the AI rows and cards, where the AIs differ (the
// headline in the combined answer), the evidence sections and the judgments
// grouped by direction. compare.ts and thread.ts decide the states; this
// only words and orders them, so it is tested without a screen.
//
// Fixes from the S5b phone check (docs/phase2-plan.md part 23):
// - R-38 the second step ('종합 답변 쓰는 중…') waits as the next step while
//   the AIs still compare; it becomes the current step only once the
//   comparison is final and the combined answer is being written.
// - R-39 a model shows without its variant ('#high') and provider
//   ('opencode-go/') everywhere.
// - R-40 every accessibility value is an object (rowValue).
// - R-41 the judgments of one direction (judge, sentences, answer) are one
//   line with how often each label came.
// - R-42 an empty evidence section says so.

import { agentName } from '@/api/agents';
import { AgentError } from '@/api/client';
import type { Comparison, Info, NumberSpan, RunPart } from '@/api/types';
import {
  type CardKind,
  cardKinds,
  type CardView,
  compareErrorText,
  compareState,
  firstShown,
  type ItemView,
  judging,
  type JudgmentLine,
  labelText,
  pieces,
  progress,
  requestLines,
  synthState,
  type UnitView,
  whyText,
} from '@/ask/compare';
import { parts, type Target, type Turn } from '@/ask/thread';
import { errorText, has, type MessageKey, t } from '@/i18n';
import type { StepView } from '@/ui/CombinedAnswer';
import type { EvidenceItemView, EvidenceSectionView, TileView, WhoPiece } from '@/ui/Evidence';
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

/** seconds with tenths, for one answer's or one judgment's time ('9.8 s'). */
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
 * once the judges are done, how long the comparison took (its slowest
 * judging request: snapshots carry no start or end times).
 */
export function turnTimes(turn: Turn): { answersMs?: number; compareMs?: number } {
  const out: { answersMs?: number; compareMs?: number } = {};
  const answers = (turn.run?.parts ?? [])
    .filter((p) => turn.agents.includes(p.agent) && p.status !== 'running' && typeof p.ms === 'number')
    .map((p) => p.ms as number);
  if (answers.length) out.answersMs = Math.max(...answers);
  const s = snapshotOf(turn);
  if (s && !judging(s)) {
    const judged = (s.requests ?? []).filter((r) => r.judge && typeof r.ms === 'number').map((r) => r.ms as number);
    if (judged.length) out.compareMs = Math.max(...judged);
  }
  return out;
}

/** metaLine: 'answers 12 s · compared in 25 s', each part when known. */
export function metaLine(turn: Turn): string {
  const { answersMs, compareMs } = turnTimes(turn);
  return [
    answersMs !== undefined ? t('ask.metaAnswers', { s: wholeSeconds(answersMs) }) : '',
    compareMs !== undefined ? t('ask.metaCompare', { s: wholeSeconds(compareMs) }) : '',
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
 * blockTitle: 종합 답변 while the AIs compare or once they did, when one is
 * coming or came (also failed); else 비교 (also before 비교하기, after a lost
 * or forgotten comparison, and with one answer).
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
 * while the AIs compare, 'Comparing… 3/6' (with 'usually 40 s' once the
 * judging is measured) and, when a combined answer is coming, its step
 * as the next one. It becomes the current step only once the comparison
 * is final and the answer waits to be written or is being written (R-38);
 * the comparison's step is then done (or failed, when every judge failed
 * and the answer is written from the answers alone). Otherwise none.
 */
export function steps(turn: Turn, info: Info | null): StepView[] {
  const st = compareState(turn);
  if (!st) return [];
  const synth: StepView = { key: 'synth', label: t('compare.step.synth'), state: 'pending' };
  if (st.kind === 'comparing') {
    const typical = info?.judging?.verified && info.judging.typicalMs ? wholeSeconds(info.judging.typicalMs) : 0;
    const compare: StepView = { key: 'compare', label: t('compare.comparingStart'), state: 'active' };
    if (st.total) compare.count = `${st.ended}/${st.total}`;
    if (typical) compare.detail = t('compare.typicalShort', { s: typical });
    return synthExpected(turn) ? [compare, synth] : [compare];
  }
  const y = synthState(turn, info);
  if ((st.kind === 'done' || st.kind === 'failed') && (y?.kind === 'waiting' || y?.kind === 'writing')) {
    const s = snapshotOf(turn);
    const compare: StepView =
      st.kind === 'done'
        ? { key: 'compare', label: t('compare.judgeCount', { n: s ? progress(s).total : 0 }), state: 'done' }
        : { key: 'compare', label: t('compare.title'), state: 'failed' };
    return [compare, { ...synth, state: 'active' }];
  }
  return [];
}

// ---- A unit's numbers ----

/**
 * numbersOf: the numbers of a unit the PC marked (its number spans), as
 * text, in order, with ' / ' between them so two numbers of one sentence
 * do not read as one (UX-2): ', ' already parts the sides of a headline
 * and ' · ' those of a who line.
 */
export function numbersOf(unit: UnitView, spans: NumberSpan[]): string {
  return pieces(
    unit.text,
    unit.math,
    spans.filter((x) => x.unit === unit.id),
  )
    .filter((p) => p.bold)
    .map((p) => (p.math ? (texToUnicode(p.math.tex) ?? p.math.tex) : p.text).trim())
    .filter(Boolean)
    .join(' / ');
}

// ---- Where the AIs differ, in the combined answer ----

/**
 * differHeadline is the highlighted part of 'AI마다 다릅니다: …': each
 * side's AIs and its numbers ('Claude 15~20년, Codex 10~15년, OpenCode
 * 8~10년'), while that is short (three sides at most, each with numbers,
 * none a whole answer); else how many AIs' answers differ, counting the
 * whole answers (COR-1). summary says it is the count, which stands
 * alone: 'AI마다 다릅니다:' in front of it would say the same twice (UX-6).
 */
export function differHeadline(item: ItemView): { text: string; summary: boolean } {
  const sides = item.sides.map((sd) => ({ names: sd.agents.join('·'), nums: numbersOf(sd.units[0], sd.numberSpans) }));
  if (sides.length <= 3 && !item.whole.length && sides.every((x) => x.nums)) {
    return { text: sides.map((x) => `${x.names} ${x.nums}`).join(', '), summary: false };
  }
  const n = new Set([...item.sides.flatMap((sd) => sd.agents), ...item.whole]).size;
  return { text: t('compare.differSummary', { n }), summary: true };
}

/** answerAgents maps a snapshot's answer ids to their AIs (codes). */
export function answerAgents(s: Comparison | undefined): Map<string, string> {
  return new Map((s?.answers ?? []).map((a) => [a.id, a.agent]));
}

/** A row under the headline: one AI's own sentence, leading to its answer. */
export type DifferRow = { key: string; agent: string; name: string; unit: UnitView; spans: NumberSpan[] };

/** differRows: every unit of every side, in the item's order. */
export function differRows(s: Comparison | undefined, item: ItemView): DifferRow[] {
  const agents = answerAgents(s);
  return item.sides.flatMap((sd) =>
    sd.units.map((u) => ({ key: u.id, agent: agents.get(u.answer) ?? u.answer, name: u.agent, unit: u, spans: sd.numberSpans })),
  );
}

// ---- The evidence ----

const numberChecks = ['numberMismatch', 'numbersAbsent'];

/** tiles are the four folded counts; null ('…') while the judges still run, except Differ. */
export function tiles(cs: CardView[], ended: boolean): TileView[] {
  return cardKinds.map((kind) => {
    const n = cs.find((c) => c.kind === kind)?.items.length ?? 0;
    const count = kind === 'differ' || ended ? n : null;
    const name = t(`compare.kind.${kind}`);
    return {
      kind,
      label: name,
      count,
      a11yLabel: count === null ? t('compare.tilePending', { kind: name }) : t('compare.tileA11y', { kind: name, n: count }),
    };
  });
}

/**
 * whoOf is an item's second line: who said it and, for Differ, what the
 * other sides say; a side that is a whole answer is its AIs' names
 * (COR-1), and with no other side at all there is no '↔'.
 */
export function whoOf(item: ItemView): WhoPiece[] {
  const [first, ...others] = item.sides;
  const unit = first.units[0];
  switch (item.kind) {
    case 'differ': {
      const names = first.agents.join(', ');
      if (!others.length && !item.whole.length) return [{ text: names }];
      const out: WhoPiece[] = [{ text: `${names} ↔ ` }];
      others.forEach((sd, i) => {
        const nums = numbersOf(sd.units[0], sd.numberSpans);
        out.push({ text: `${i ? ' · ' : ''}${sd.agents.join(', ')}${nums ? ' ' : ''}` });
        if (nums) out.push({ text: nums, bold: true });
      });
      item.whole.forEach((name, i) => out.push({ text: `${others.length || i ? ' · ' : ''}${name}` }));
      return out;
    }
    case 'check': {
      const why = item.check.map(whyText).filter((x): x is string => !!x);
      return [{ text: [t('compare.sentenceOf', { name: unit.agent }), ...why].join(' · ') }];
    }
    case 'agree': {
      const names = [...new Set(item.sides.flatMap((sd) => sd.agents))];
      return [{ text: names.join(' · ') }];
    }
    default:
      return [{ text: t('compare.only', { name: first.agents[0] ?? unit.agent }) }];
  }
}

/** sectionCaption: what a section means, short; Check this says the numbers differ when every reason is a number check. */
export function sectionCaption(kind: CardKind, items: ItemView[]): string {
  if (kind === 'check' && items.length && items.every((it) => it.check.length > 0 && it.check.every((c) => numberChecks.includes(c)))) {
    return t('compare.numbersDiffer');
  }
  return t(`compare.short.${kind}`);
}

/**
 * evidenceSections are the four sections, always all four (F-Result-
 * Evidence): their first three items, then 'Show n more'; Differ and Check
 * this lead to the pair sheet. While judges run, the sections other than
 * Differ have nothing yet and say so; once every judge ended an empty
 * section says there is nothing (R-42), Differ in its own words (or, when
 * no sentence could be compared at all, that).
 */
export function evidenceSections(o: {
  cards: CardView[];
  ended: boolean;
  partial: boolean;
  failed: string[];
  more: readonly CardKind[];
  onMore: (kind: CardKind) => void;
  onPair: (key: string) => void;
}): EvidenceSectionView[] {
  const none = o.ended && o.cards.every((c) => c.items.length === 0);
  return cardKinds.map((kind) => {
    const all = o.cards.find((c) => c.kind === kind)?.items ?? [];
    const shown = o.more.includes(kind) ? all : all.slice(0, firstShown);
    const rest = all.length - shown.length;
    const items: EvidenceItemView[] = shown.map((it) => {
      const side = it.sides[0];
      const v: EvidenceItemView = { key: it.key, unit: side.units[0], spans: side.numberSpans, who: whoOf(it) };
      if (it.missing.length) v.note = t('compare.cantCompare', { names: it.missing.join(', ') });
      v.a11yLabel = spoken([v.unit.text, v.who.map((p) => p.text).join(''), v.note ?? '']);
      if (kind === 'differ' || kind === 'check') v.onPress = () => o.onPair(it.key);
      return v;
    });
    const sec: EvidenceSectionView = {
      kind,
      label: t(`compare.kind.${kind}`),
      count: kind === 'differ' || o.ended ? all.length : null,
      caption: sectionCaption(kind, all),
      items,
    };
    if (rest > 0) sec.more = { label: t('compare.more', { n: rest }), onPress: () => o.onMore(kind) };
    if (!all.length) sec.empty = emptyLine(kind, o.ended, none, o.partial, o.failed);
    return sec;
  });
}

// spoken joins lines into what a screen reader says, with a stop between them unless one is there.
function spoken(lines: string[]): string {
  return lines
    .map((l) => l.trim())
    .filter(Boolean)
    .reduce((out, l) => (out ? `${out}${/[.!?。…:)]$/.test(out) ? ' ' : '. '}${l}` : l), '');
}

function emptyLine(kind: CardKind, ended: boolean, none: boolean, partial: boolean, failed: string[]): string {
  if (!ended) return t('compare.comparingStart');
  if (kind !== 'differ') return t('compare.none');
  if (none) return t('compare.noCore');
  if (!partial) return t('compare.noDiffer');
  return [t('compare.noDifferPartial'), failed.length ? t('compare.cantCompare', { names: failed.join(', ') }) : ''].filter(Boolean).join(' ');
}

// ---- The pair sheet ----

/** One direction's judgments as one line (R-41): who judged whose sentences against whose answer, and the labels. */
export type JudgmentGroup = { key: string; text: string };

/**
 * groupJudgments makes one line per (judge, sentences, answer): an item
 * with two of Claude's sentences has one line for each direction, not one
 * per sentence. Each label shows once, with how often it came when more
 * than once ('Same ×2'), and 'not sure' after the unsure ones.
 */
export function groupJudgments(lines: JudgmentLine[]): JudgmentGroup[] {
  type Label = { label: string; unsure: boolean; n: number };
  const groups: Array<{ judge: string; from: string; against: string; labels: Label[] }> = [];
  for (const l of lines) {
    let g = groups.find((x) => x.judge === l.judge && x.from === l.from && x.against === l.against);
    if (!g) {
      g = { judge: l.judge, from: l.from, against: l.against, labels: [] };
      groups.push(g);
    }
    let e = g.labels.find((x) => x.label === l.label && x.unsure === l.unsure);
    if (!e) {
      e = { label: l.label, unsure: l.unsure, n: 0 };
      g.labels.push(e);
    }
    e.n++;
  }
  return groups.map((g) => {
    const label = g.labels
      .map((e) => {
        const word = labelText(e.label);
        const base = e.n > 1 ? t('compare.labelTimes', { label: word, n: e.n }) : word;
        return e.unsure ? `${base} · ${t('compare.unsure')}` : base;
      })
      .join(', ');
    return { key: JSON.stringify([g.judge, g.from, g.against]), text: t('compare.judgment', { judge: g.judge, from: g.from, against: g.against, label }) };
  });
}

// ---- The judging details sheet ----

/** One judging request in the sheet: 'Claude → Codex' and how it went. */
export type RequestRow = { id: string; text: string; a11y: string; status: string; failed: boolean };
/** The requests one AI judged (agent '' for pairs no AI could judge). */
export type JudgeGroup = { key: string; agent: string; name: string; model: string; rows: RequestRow[] };

/**
 * judgeGroups puts a comparison's requests under the AI that judged them,
 * in the turn's AI order (others after), with the pairs no AI could judge
 * last. Each row reads 'from → against' with its time, its state or why it
 * failed in the AI's words.
 */
export function judgeGroups(s: Comparison, order: string[]): JudgeGroup[] {
  const lines = requestLines(s);
  const groups = new Map<string, JudgeGroup>();
  (s.requests ?? []).forEach((r, i) => {
    const l = lines[i];
    if (!l) return;
    const key = r.judge || '';
    let g = groups.get(key);
    if (!g) {
      g = { key: key || '-', agent: r.judge, name: r.judge ? agentName(r.judge) : '', model: displayModel(r.model), rows: [] };
      groups.set(key, g);
    }
    if (!g.model && r.model) g.model = displayModel(r.model);
    const status = requestStatus(l);
    if (!r.judge) {
      g.rows.push({ id: r.id, text: `${l.from} ↔ ${l.against}`, a11y: `${l.from} ↔ ${l.against}, ${status.text}`, status: status.text, failed: true });
      return;
    }
    const only = l.judgeOnly ? ` ${t('compare.judgeOnly')}` : '';
    g.rows.push({
      id: r.id,
      text: t('compare.requestShort', { from: l.from, against: l.against }) + only,
      a11y: `${t('compare.requestA11y', { from: l.from, against: l.against })}${only}, ${status.text}`,
      status: status.text,
      failed: !!status.failed,
    });
  });
  const at = (agent: string) => {
    if (!agent) return Number.MAX_SAFE_INTEGER;
    const i = order.indexOf(agent);
    return i < 0 ? order.length : i;
  };
  return [...groups.values()].map((g, i) => ({ g, i })).sort((a, b) => at(a.g.agent) - at(b.g.agent) || a.i - b.i).map((x) => x.g);
}

function requestStatus(l: { judge: string; status: string; ms?: number; error?: { code: string; message?: string } }): { text: string; failed?: boolean } {
  if (!l.judge) return { text: compareErrorText('no_judge'), failed: true };
  switch (l.status) {
    case 'done':
      return { text: l.ms !== undefined ? tenths(l.ms) : '' };
    case 'queued':
      return { text: t('compare.req.queued') };
    case 'running':
      return { text: t('compare.req.running') };
    case 'failed':
      return { text: compareErrorText(l.error?.code ?? 'internal', l.error?.message), failed: true };
    default:
      return { text: l.status };
  }
}

/** judgedCount: the judging requests of a comparison (no_judge records are not judgments). */
export function judgedCount(s: Comparison): number {
  return progress(s).total;
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

/** onlyTarget: the AI a follow-up goes to alone, or null for everyone. */
export const onlyTarget = (to: Target): string | null => (to === 'all' ? null : to.agent);

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
