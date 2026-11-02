// An answer's trailing extras (#4): a closing section of messages or
// questions to send next, or a last paragraph offering more. The agent
// leaves them out of judging and the combined answer (judge.ExtrasStart in
// agent/internal/judge/extras.go); the phone folds them under the answer.
// Same rules, same cases (agent/internal/judge/testdata/extras.json):
// change both together. Offsets here are UTF-16, the agent's bytes; the
// cases compare the text, so both read the same.

const HEADING_CHARS = 40;
const OFFER_CHARS = 200;

// No \b at the ends: it is ASCII-only and Korean ends a word with a
// non-word character.
const heading =
  /^(다음에 (쓸|보낼|물어볼) (문장|말|질문)|다음 질문|후속 질문|추천 질문|이어서 (물어볼|할) (만한 )?(질문|말)|더 물어볼 (만한 )?(질문|것)|next (message|prompt)|suggested (follow-?up|question|prompt|next message)|follow-?up question|you (might|could|may) also ask|(questions|things) to ask next|what to ask next)/i;
const offerOpen =
  /^(원하시면|원하면|원한다면|필요하시면|필요하면|필요하다면|궁금한 (점|게) (있으시면|있으면)|더 궁금하|if you('d| would)? (like|want|need)|let me know if|want me to|would you like me to|shall i|i can also)/i;
const offerVerb =
  /(드릴게요|드릴까요|드릴 수 있|드리겠|해 ?줄게|해 ?줄까|알려 ?줄게|알려 ?드릴|말씀해 ?주세요|말해 ?줘|물어봐 ?주세요|\bi can\b|\bi could\b|\bi'll\b|\bi will\b|\blet me know\b|\bwant me to\b|\bwould you like me to\b|\bshall i\b|\bhappy to\b)/i;
const sectionNumber = /^\d{1,2}[.)]\s*/;
const listLine = /^\s*([-*+>]\s|\d{1,3}[.)]\s)/;
const rule = /^ {0,3}([-*_])( *[-*_]){2,} *$/;
const fenceOpen = /^ {0,3}(`{3,}|~{3,})/;

type Line = { start: number; end: number; text: string; blank: boolean; code: boolean };

// Only the last MAX_LINES lines are read: extras are short, and a long
// answer is not read whole again.
const MAX_LINES = 120;

// fenceAfter is the open fence after line, given the one before it ('' outside code).
function fenceAfter(fence: string, line: string): string {
  if (fence) {
    const tt = line.trim();
    return tt.startsWith(fence) && tt.split(fence[0]).join('') === '' ? '' : fence;
  }
  return fenceOpen.exec(line)?.[1] ?? '';
}

function lines(text: string): Line[] {
  let from = 0;
  let breaks = 0;
  for (let i = text.indexOf('\n'); i >= 0 && breaks < MAX_LINES; i = text.indexOf('\n', i + 1)) breaks++;
  if (breaks >= MAX_LINES) {
    let pos = text.length;
    for (let k = 0; k < MAX_LINES; k++) pos = text.lastIndexOf('\n', pos - 1);
    from = pos + 1;
  }
  let fence = '';
  for (let pos = 0; pos < from; ) {
    const i = text.indexOf('\n', pos);
    const end = i < 0 || i > from ? from : i;
    const raw = text.slice(pos, end);
    const t = raw.replace(/^ +/, '');
    if (raw.length - t.length <= 3 && (t.startsWith('```') || t.startsWith('~~~'))) fence = fenceAfter(fence, raw.replace(/\r$/, ''));
    pos = end + 1;
  }
  const out: Line[] = [];
  for (let pos = from; pos < text.length; ) {
    const i = text.indexOf('\n', pos);
    const end = i < 0 ? text.length : i;
    const next = i < 0 ? text.length : i + 1;
    const raw = text.slice(pos, end).replace(/\r$/, '');
    const was = fence;
    fence = fenceAfter(fence, raw);
    out.push({ start: pos, end: pos + raw.length, text: raw, blank: raw.trim() === '', code: was !== '' || fence !== '' });
    pos = next;
  }
  return out;
}

const trimMarks = (s: string) => s.replace(/^[*_ ]+|[*_ ]+$/g, '');

function headingText(line: string): string | null {
  let s = line.trim();
  if (s.startsWith('#')) {
    s = s.replace(/^#+/, '');
    if (s !== '' && s[0] !== ' ') return null;
  } else if (s.length > 4 && ((s.startsWith('**') && s.endsWith('**')) || (s.startsWith('__') && s.endsWith('__')))) {
    // only bold
  } else if (!s.endsWith(':') && !s.endsWith('：')) {
    return null;
  }
  s = trimMarks(s.trim());
  s = s.replace(/[:：]$/, '').trim();
  s = trimMarks(s).replace(sectionNumber, '');
  if (s === '' || [...s].length > HEADING_CHARS) return null;
  return s;
}

function isOffer(para: string): boolean {
  const s = trimMarks(para.trim());
  return [...s].length <= OFFER_CHARS && offerOpen.test(s) && offerVerb.test(s);
}

/** extrasStart is where text's trailing extras begin, or text.length when it has none. */
export function extrasStart(text: string): number {
  const ls = lines(text);
  let cut = text.length;
  for (let i = ls.length - 1; i >= 0; i--) {
    const l = ls[i];
    if (l.code || l.blank || listLine.test(l.text)) continue;
    const h = headingText(l.text);
    if (h === null) continue;
    if (heading.test(h)) cut = l.start;
    break;
  }

  let last = -1;
  for (let i = ls.length - 1; i >= 0; i--) {
    if (ls[i].start < cut && !ls[i].blank) {
      last = i;
      break;
    }
  }
  if (last >= 0 && !ls[last].code && !rule.test(ls[last].text)) {
    let first = last;
    while (first > 0 && !ls[first - 1].blank && !ls[first - 1].code && !rule.test(ls[first - 1].text)) first--;
    if (isOffer(text.slice(ls[first].start, ls[last].end))) cut = ls[first].start;
  }

  for (let i = ls.length - 1; i >= 0; i--) {
    const l = ls[i];
    if (l.start >= cut) continue;
    if (!l.blank && (l.code || !rule.test(l.text))) break;
    cut = l.start;
  }
  if (cut === text.length || text.slice(0, cut).trim() === '') return text.length;
  // The answer ends at its last character, not at a line break.
  return text.slice(0, cut).replace(/[ \t\r\n]+$/, '').length;
}

/** splitExtras gives an answer's own part and its trailing extras ('' when none). */
export function splitExtras(text: string): { main: string; extras: string } {
  const at = extrasStart(text);
  return { main: text.slice(0, at), extras: text.slice(at).trim() };
}
