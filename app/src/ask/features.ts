// Features that are built but turned off.
//
// refine (질문 정제, milestone C): the moderator reads each question first
// and an answer format goes after it to every AI. Off since 2026-11-04:
// the C4 quality experiments found the formats did not make the combined
// answer better, and the combined answer reads the question itself. The
// code stays. Off, Settings hides its switch and runs ask for no refining;
// the agent ignores refine as well unless it is turned on there
// (api.Options.Refine). Tests turn it on to keep its code working.
//
// judging (비교 판정, P2-3): the AIs judge each other's answers, and the
// screen shows the cards (근거), 'AI 답이 다른 곳' and who judged. Off since
// 2026-11-05 (C6, #19, the user's decision): the moderator alone writes the
// combined answer from the answers, for two AIs as for more, and the screen
// shows that answer and the AIs' own answers. The agent judges no more
// either (api.Options.NoJudging). The code stays until the user has seen it
// on the phone; the tests turn judging on (src/test/setup.ts) except those
// of C6.
export const features = { refine: false, judging: false };
