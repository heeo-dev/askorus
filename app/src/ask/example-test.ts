// The example conversation the app carries (#51): it is a whole one (three
// answers, the combined answer, a debate with the answer after it) in both
// languages, in the shape the phone keeps, and nothing in it points at the
// PC it was asked on.
import { compareState, synthState } from './compare';
import { exampleDate, exampleTurns } from './example';
import { partViews } from './present';

const langs = ['en', 'ko'] as const;

test.each(langs)('%s: one question, every AI answered, a combined answer, a debate that is done', (lang) => {
  const turns = exampleTurns(lang);
  expect(turns).toHaveLength(1);
  const turn = turns[0];
  expect(turn.question.trim().length).toBeGreaterThan(10);
  expect(turn.agents).toEqual(['claude', 'codex', 'opencode']);
  expect(turn.error).toBeUndefined();
  expect(turn.queued).toBeUndefined();

  // Drawn with no PC's info, as the example screen draws it.
  const views = partViews(turn, turn.at ?? 0, null);
  expect(views.map((p) => p.state)).toEqual(['done', 'done', 'done']);
  for (const p of views) {
    expect((p.part.answer ?? '').trim().length).toBeGreaterThan(40);
    expect(p.model).toBeTruthy();
  }
  expect(compareState(turn)).toBeTruthy();
  const synth = synthState(turn, null);
  expect(synth?.kind).toBe('done');
  expect(synth?.kind === 'done' && synth.text.trim().length).toBeGreaterThan(40);

  const debate = turn.debate?.snap;
  expect(turn.debate?.error).toBeUndefined();
  expect(debate?.status).toBe('done');
  expect(debate?.pollAfterMs).toBeUndefined();
  expect(debate?.after?.text.trim().length).toBeGreaterThan(40);
  // Something was really debated: a point with turns that have words.
  const spoken = (debate?.points ?? []).flatMap((p) => p.turns ?? []).filter((x) => x.status === 'done' && (x.text ?? '').trim());
  expect(spoken.length).toBeGreaterThanOrEqual(2);
});

test('the question and the answers are in the language of the example', () => {
  const hangul = /[가-힣]/;
  const [en] = exampleTurns('en');
  const [ko] = exampleTurns('ko');
  expect(hangul.test(en.question)).toBe(false);
  expect(hangul.test(ko.question)).toBe(true);
  for (const p of ko.run?.parts ?? []) expect(hangul.test(p.answer ?? '')).toBe(true);
  expect(hangul.test(ko.compare?.snap?.synthesis?.text ?? '')).toBe(true);
  expect(exampleDate.en).toMatch(/2026/);
  expect(exampleDate.ko).toMatch(/2026년/);
});

test.each(langs)('%s: nothing points at the PC it was asked on', (lang) => {
  const [turn] = exampleTurns(lang);
  const text = JSON.stringify(turn);
  // No AI's session (a conversation on that PC), no ids the PC gave.
  expect(text).not.toMatch(/"session"\s*:/);
  expect(turn.id).toBe(`example-${lang}`);
  expect(turn.run?.runId).toBe('example-run');
  expect(turn.compare?.id).toBe('example-comparison');
  expect(turn.debate?.id).toBe('example-debate');
  expect(text).not.toMatch(/\b[0-9a-f]{32}\b/);
  expect(text).not.toMatch(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/i);
  // No place on a PC or a network, no mail address.
  expect(text).not.toMatch(/\.ts\.net/i);
  expect(text).not.toMatch(/[A-Za-z]:\\\\/);
  expect(text).not.toMatch(/[\\/](Users|home)[\\/]/i);
  expect(text).not.toMatch(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/);
  // It carries no conversation record and no refining: a first question.
  expect(turn.req.context).toBeUndefined();
});
