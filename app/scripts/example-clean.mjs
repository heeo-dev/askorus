// Prepares a kept turn as the app's example conversation (#51, src/ask/example.ts).
//   node scripts/example-clean.mjs <turn.json> <en|ko>
// The turn is one the phone keeps (thread.ts Turn), from a question really
// asked with the app. The answers are left exactly as they came. Only what
// points at the PC it was asked on goes: each AI's session id (it names a
// conversation on that PC) and the ids of the run, the comparison and the
// debate, which become fixed names. The script stops if anything that
// looks like a place on a PC or a network is left in the text.

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const [src, lang] = process.argv.slice(2);
if (!src || !['en', 'ko'].includes(lang)) {
  console.error('usage: node scripts/example-clean.mjs <turn.json> <en|ko>');
  process.exit(2);
}
const turn = JSON.parse(fs.readFileSync(src, 'utf8'));

const need = (ok, what) => {
  if (!ok) {
    console.error(`not usable as the example: ${what}`);
    process.exit(1);
  }
};
const parts = turn.run?.parts ?? [];
need(turn.run?.status === 'done' && parts.length >= 2 && parts.every((p) => p.status === 'done' && p.answer), 'every AI must have answered');
need(turn.compare?.snap?.synthesis?.status === 'done' && turn.compare.snap.synthesis.text, 'the combined answer must be done');
need(turn.debate?.snap?.status === 'done' && turn.debate.snap.after?.text, 'the debate must be done, with the answer after it');
need(!turn.error && !turn.compare.error && !turn.debate.error, 'no errors');

const id = `example-${lang}`;
turn.id = id;
turn.req.clientRunId = id;
for (const a of turn.req.agents ?? []) delete a.session;
turn.run.runId = 'example-run';
if (turn.run.comparisonId) turn.run.comparisonId = 'example-comparison';
for (const p of parts) delete p.session;
delete turn.compare.key;
turn.compare.id = 'example-comparison';
turn.compare.snap.comparisonId = 'example-comparison';
if (turn.compare.snap.runId) turn.compare.snap.runId = 'example-run';
delete turn.debate.key;
turn.debate.id = 'example-debate';
turn.debate.snap.debateId = 'example-debate';
// Nothing is still coming.
delete turn.run.pollAfterMs;
delete turn.compare.snap.pollAfterMs;
delete turn.debate.snap.pollAfterMs;

const text = JSON.stringify(turn, null, 2) + '\n';
const leaks = [
  ['a session id', /"session"\s*:/],
  ['a tailnet name', /\.ts\.net/i],
  ['a Tailscale address', /\b100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\.\d{1,3}\.\d{1,3}\b/],
  ['a Windows path', /[A-Za-z]:\\\\/],
  ['a home folder', /[\\/](Users|home)[\\/]/i],
  ['a mail address', /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/],
  ['an id of 32 hex signs', /\b[0-9a-f]{32}\b/],
  ['a UUID', /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/i],
];
for (const [what, re] of leaks) need(!re.test(text), `${what} is left in the text`);

const out = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'src', 'ask', `example-${lang}.json`);
fs.writeFileSync(out, text);
console.log(`${out}: ${parts.length} answers, ${turn.debate.snap.points.length} debate points, ${text.length} signs`);
