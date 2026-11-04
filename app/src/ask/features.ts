// Features that are built but turned off.
//
// refine (질문 정제, milestone C): the moderator reads each question first
// and an answer format goes after it to every AI. Off since 2026-11-04:
// the C4 quality experiments found the formats did not make the combined
// answer better, and the combined answer reads the question itself. The
// code stays. Off, Settings hides its switch and runs ask for no refining;
// the agent ignores refine as well unless it is turned on there
// (api.Options.Refine). Tests turn it on to keep its code working.
export const features = { refine: false };
