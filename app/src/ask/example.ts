// The example conversation the app carries (#51): one question with the
// answers of three AIs, the moderator's combined answer and a debate, to
// look at before a PC is paired. Someone who has just installed the app
// sees what it does, and so does a store reviewer who has no PC to pair
// (the report button of #50 sits under an answer, and without a PC there
// is no answer to see).
//
// Nothing here is made up: each file is a turn exactly as the phone keeps
// one (thread.ts Turn), from a question really asked with this app's own
// code on the day in exampleDate, one in English and one in Korean. Only
// what pointed at the PC it was asked on is taken out (session and run
// ids, see scripts/example-clean.mjs). The example is drawn by the same
// parts as a real conversation (ui/TurnView); it is never kept in the
// history and no PC is asked anything.

import type { Lang } from '@/i18n';

import en from './example-en.json';
import ko from './example-ko.json';
import type { Turn } from './thread';

/** The day the example's answers were received, as each language writes it. */
export const exampleDate: Record<Lang, string> = { en: 'November 11, 2026', ko: '2026년 11월 11일' };

/** exampleTurns is the example conversation in the app's language. */
export function exampleTurns(lang: Lang): Turn[] {
  return [(lang === 'ko' ? ko : en) as unknown as Turn];
}
