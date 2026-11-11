// License files are wrapped for an 80-column screen. On a phone each of
// those lines wraps once more and the text reads as long and short lines in
// turn. reflow() joins the lines of one paragraph again, for showing only:
// the stored text (licenses.json) stays as the package has it.
//
// A line is joined to the one before when that one is long enough to have
// been wrapped (40 characters or more), and the line itself is not indented
// less, and does not open a list item or a copyright line. Blank lines,
// short lines (titles, names), drawn lines, web addresses on their own line
// and list items stay as they are.

const wrapped = 40;
// "Copyright 2015 A", "Copyright (c) A", not "copyright notice that is …".
const opens = /^(?:[-*•+]\s|\(?[0-9]{1,2}[.)]\s|\(?[a-zA-Z][.)]\s|\([ivx]+\)\s|copyright\s+(?:\(c\)|©|\d{4}|\[)|\(c\)|©)/i;
const holder = /^(?:copyright\s+(?:\(c\)|©|\d{4}|\[)|\(c\)|©)/i;
const address =/^<?https?:\/\/\S+>?[.,]?$/i;

const indentOf = (line: string) => line.length - line.trimStart().length;

export function reflow(text: string): string {
  const out: string[] = [];
  let indent = 0; // of the first line of the paragraph being built
  let last = 0; // length of the line read last, without its indentation
  for (const raw of text.replace(/\r\n?/g, '\n').split('\n')) {
    const line = raw.replace(/\s+$/, '');
    const body = line.trimStart();
    const prev = out.length ? out[out.length - 1] : '';
    // A drawn line ("-----") or an address alone on its line stands alone.
    const alone = !/[A-Za-z0-9]/.test(body) || address.test(body);
    const join = body !== '' && !alone && prev !== '' && last >= wrapped && indentOf(line) >= indent && !opens.test(body);
    if (join) {
      out[out.length - 1] = `${prev} ${body}`;
    } else {
      out.push(line);
      indent = indentOf(line);
    }
    // Nothing is joined onto a copyright line ("All rights reserved." below
    // it keeps its line).
    last = alone || holder.test(body) ? 0 : body.length;
  }
  return out.join('\n');
}
