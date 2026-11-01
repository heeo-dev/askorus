// The pretend agent's answer comparison (P2-3), for screen tests: it splits
// answers into units, judges one direction of a pair by a simple rule, and
// builds the cards. These are not the agent's rules (agent/internal/judge),
// only close enough that the screens get snapshots shaped exactly like
// GET /v1/comparisons/{id}.
//
// The judge rule: a unit's subject is its text in lower case with every
// number as '#' and no punctuation. The same subject with the same numbers
// is 'same', the same subject with other numbers 'opposite', no unit of the
// same subject 'absent'. So "It has 9.4 million people." and "It has 9.7
// million people." differ.

import type { CardCounts, CardItem, CompareAnswer, JudgeRequest, Judgment, NumberSpan, Unit, UnitMath } from '@/api/types';
import { inlineMathSpans } from '@/ui/inlineMath';

const digitRun = /\d+(?:[.,]\d+)*/g;

/** subjectOf: what a unit is about, numbers left out. */
export function subjectOf(text: string): string {
  return text
    .toLowerCase()
    .replace(digitRun, '#')
    .replace(/[^\p{L}\p{N}#\s]/gu, '')
    .replace(/\s+/g, ' ')
    .trim();
}

/** digitsOf: the numbers of a unit, in order. */
export function digitsOf(text: string): string {
  return (text.match(digitRun) ?? []).join(' ');
}

/** numberSpans: where a unit's numbers are, to show them bold. */
export function numberSpans(unit: Unit): NumberSpan[] {
  return [...unit.text.matchAll(digitRun)].map((m) => ({ unit: unit.id, start: m.index ?? 0, end: (m.index ?? 0) + m[0].length }));
}

// plainText takes the backticks out of a unit, as the agent does, and finds
// its math outside code only: "`$HOME/bin:$PATH`" stays text, with no span.
function plainText(raw: string): { text: string; math: UnitMath[] } {
  let text = '';
  const math: UnitMath[] = [];
  const prose = (seg: string) => {
    const base = text.length;
    for (const s of inlineMathSpans(seg)) math.push({ start: base + s.start, end: base + s.end, display: s.display });
    text += seg;
  };
  const code = /`([^`]*)`/g;
  let at = 0;
  for (let m = code.exec(raw); m; m = code.exec(raw)) {
    prose(raw.slice(at, m.index));
    text += m[1];
    at = m.index + m[0].length;
  }
  prose(raw.slice(at));
  return { text, math };
}

/**
 * splitUnits splits an answer at sentence ends and line breaks. A line
 * that starts with "- " is an item (the mark left out). start and end are
 * UTF-16 offsets in the answer; units after maxUnits are not judged.
 */
export function splitUnits(answerId: string, text: string, maxUnits: number): Unit[] {
  const cuts: Array<[number, number]> = [];
  const re = /(?<=[.!?。])\s+|\n+/g;
  let from = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    cuts.push([from, m.index]);
    from = m.index + m[0].length;
  }
  cuts.push([from, text.length]);
  const units: Unit[] = [];
  for (const cut of cuts) {
    let [start, end] = cut;
    while (start < end && /\s/.test(text[start])) start++;
    while (end > start && /\s/.test(text[end - 1])) end--;
    if (start >= end) continue;
    let raw = text.slice(start, end);
    let kind = 'sentence';
    if (raw.startsWith('- ')) {
      kind = 'item';
      raw = raw.slice(2);
      start += 2;
    }
    const n = units.length + 1;
    const { text: plain, math } = plainText(raw);
    const u: Unit = { id: `${answerId}:${n}`, n, kind, text: plain, start, end, judged: n <= maxUnits };
    if (math.length) u.math = math;
    units.push(u);
  }
  return units;
}

/** judgePair judges the units of `from` against the answer `against`. */
export function judgePair(
  from: CompareAnswer,
  against: CompareAnswer,
  o: { request: string; judge: string; unsure: boolean; core: boolean },
): Judgment[] {
  return from.units
    .filter((u) => u.judged)
    .map((u) => {
      const subject = subjectOf(u.text);
      const peers = against.units.filter((v) => subjectOf(v.text) === subject);
      const same = peers.find((v) => digitsOf(v.text) === digitsOf(u.text));
      const label = same ? 'same' : peers.length ? 'opposite' : 'absent';
      const evidence = same ? [same.id] : peers.length ? [peers[0].id] : [];
      return {
        unit: u.id,
        against: against.id,
        request: o.request,
        judge: o.judge,
        core: o.core,
        label,
        evidence,
        unsure: o.unsure,
        flags: o.unsure ? ['unsure'] : [],
      };
    });
}

const unique = (xs: string[]) => xs.filter((x, i) => xs.indexOf(x) === i);

/**
 * aggregate builds the cards from the judgments so far. Differ items come
 * at once (keyed by their first opposite judgment, so the key stays while
 * judges finish); Check this, Agree and Only one only once every request
 * ended (ended). A unit shows when a judgment calls it core or it is in an
 * opposite judgment.
 */
export function aggregate(
  answers: CompareAnswer[],
  requests: JudgeRequest[],
  judgments: Judgment[],
  ended: boolean,
): { items: CardItem[]; counts: CardCounts } {
  const units = new Map<string, { unit: Unit; answer: CompareAnswer }>();
  for (const a of answers) for (const u of a.units) units.set(u.id, { unit: u, answer: a });
  const order = answers.flatMap((a) => a.units.filter((u) => u.judged).map((u) => u.id));
  const of = (id: string) => judgments.filter((j) => j.unit === id);
  const inOpposite = new Set<string>();
  for (const j of judgments) {
    if (j.label !== 'opposite') continue;
    inOpposite.add(j.unit);
    j.evidence.forEach((e) => inOpposite.add(e));
  }
  const shown = (id: string) => inOpposite.has(id) || of(id).some((j) => j.core);
  const answerOf = (id: string) => units.get(id)?.answer.id ?? '';
  const items: CardItem[] = [];
  const placed = new Set<string>();

  // Differ: one item per subject with an opposite, a side per number.
  const subjects: Array<{ subject: string; key: string }> = [];
  for (const j of judgments) {
    const u = units.get(j.unit);
    if (j.label !== 'opposite' || !u) continue;
    const subject = subjectOf(u.unit.text);
    if (!subjects.some((x) => x.subject === subject)) subjects.push({ subject, key: `d:${j.unit}~${j.evidence[0] ?? ''}` });
  }
  for (const { subject, key } of subjects) {
    const sides: Array<{ digits: string; agents: string[]; units: string[]; numberSpans: NumberSpan[] }> = [];
    for (const id of order) {
      const u = units.get(id);
      if (!u || placed.has(id) || subjectOf(u.unit.text) !== subject) continue;
      const digits = digitsOf(u.unit.text);
      let side = sides.find((x) => x.digits === digits);
      if (!side) {
        side = { digits, agents: [], units: [], numberSpans: [] };
        sides.push(side);
      }
      if (!side.agents.includes(u.answer.id)) side.agents.push(u.answer.id);
      side.units.push(id);
      side.numberSpans.push(...numberSpans(u.unit));
      placed.add(id);
    }
    items.push({
      key,
      kind: 'differ',
      positions: sides.map((x) => ({ agents: x.agents, units: x.units, numberSpans: x.numberSpans })),
      numbersDiffer: sides.length > 1,
      check: [],
      missing: [],
    });
  }

  if (ended) {
    // Check this: a judge was not sure. A stand-in for the screens: since S6 the agent sends an unsure
    // same or absent here only when its number check fires, and names that check as the reason.
    for (const id of order) {
      if (placed.has(id) || !shown(id) || !of(id).some((j) => j.unsure)) continue;
      items.push({ key: `c:${id}`, kind: 'check', positions: [{ agents: [answerOf(id)], units: [id] }], check: ['unsure'], missing: [] });
      placed.add(id);
    }
    // Agree: a unit judged the same and the units it is directly tied to.
    const agreeOf = new Map<string, CardItem>();
    for (const id of order) {
      if (placed.has(id) || !shown(id)) continue;
      const sames = judgments.filter((j) => j.label === 'same' && (j.unit === id || j.evidence.includes(id)));
      if (!sames.length) continue;
      const linked = unique(sames.flatMap((j) => (j.unit === id ? j.evidence : [j.unit]))).filter((x) => x !== id && units.has(x));
      const group = [id, ...linked.filter((x) => !placed.has(x))];
      const agents = unique(group.map(answerOf));
      if (agents.length >= 2) {
        const item: CardItem = { key: `a:${id}`, kind: 'agree', positions: [{ agents, units: group }], check: [], missing: [] };
        items.push(item);
        for (const x of group) {
          placed.add(x);
          agreeOf.set(x, item);
        }
        continue;
      }
      // Tied only to units already in an Agree item: it joins that item (the older key stays).
      const host = linked.map((x) => agreeOf.get(x)).find((x): x is CardItem => !!x);
      if (!host) continue;
      host.positions[0].units.push(id);
      if (!host.positions[0].agents.includes(answerOf(id))) host.positions[0].agents.push(answerOf(id));
      placed.add(id);
      agreeOf.set(id, host);
    }
    // Only one: every judgment says the others do not mention it.
    for (const id of order) {
      if (placed.has(id) || !shown(id)) continue;
      const js = of(id);
      if (!js.length || !js.every((j) => j.label === 'absent')) continue;
      const a = answerOf(id);
      const missing = unique(requests.filter((r) => r.from === a && r.status === 'failed').map((r) => r.against));
      items.push({ key: `o:${id}`, kind: 'onlyOne', positions: [{ agents: [a], units: [id] }], check: [], missing });
      placed.add(id);
    }
  }

  const count = (kind: string) => items.filter((i) => i.kind === kind).length;
  return {
    items,
    counts: {
      differ: count('differ'),
      check: count('check'),
      agree: count('agree'),
      onlyOne: count('onlyOne'),
      notCompared: order.filter((id) => !of(id).length).length,
      notJudged: answers.reduce((n, a) => n + a.notJudged, 0),
    },
  };
}

/**
 * combinedText is the pretend writer's combined answer (S5b): a first line,
 * then for every Differ item what each side says, after its AIs' names
 * (named) or with no name at all (as a writer that leaves them out). A
 * writer without the comparison's results (judged false) says so instead.
 */
export function combinedText(o: {
  items: CardItem[];
  answers: CompareAnswer[];
  name: (agent: string) => string;
  named: boolean;
  judged: boolean;
}): string {
  const lines = ['Fake combined answer.'];
  if (!o.judged) return [...lines, 'Written from the answers alone.'].join('\n\n');
  const text = new Map(o.answers.flatMap((a) => a.units.map((u) => [u.id, u.text] as const)));
  for (const it of o.items) {
    if (it.kind !== 'differ') continue;
    const sides = it.positions.map((p) => {
      const said = text.get(p.units[0]) ?? '';
      return o.named ? `${sideNames(p.agents, o.answers, o.name).join(', ')}: ${said}` : said;
    });
    lines.push(`The AIs differ. ${sides.join(' ')}`);
  }
  if (lines.length === 1) lines.push('The AIs agree.');
  return lines.join('\n\n');
}

// sideNames: the display names of a side's answers.
function sideNames(ids: string[], answers: CompareAnswer[], name: (agent: string) => string): string[] {
  return ids.map((id) => name(answers.find((a) => a.id === id)?.agent ?? id));
}

/** unnamedSides counts the Differ sides none of whose AI names the text mentions (any case), as the agent does. */
export function unnamedSides(items: CardItem[], answers: CompareAnswer[], name: (agent: string) => string, text: string): number {
  const lower = text.toLowerCase();
  return items
    .filter((it) => it.kind === 'differ')
    .flatMap((it) => it.positions)
    .filter((p) => !sideNames(p.agents, answers, name).some((n) => lower.includes(n.toLowerCase()))).length;
}
