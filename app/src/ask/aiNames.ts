// The AIs a combined answer names (#6). synth-v2 writes the name of an AI
// that answered in double braces, "{{Claude}}", and leaves a product or
// company the question is about bare, so the phone can draw the AIs as
// chips and keep "Claude가 48~49개라고 합니다" (the AI) apart from "Claude
// Code의 스킬" (the subject). Copying and screen readers get the names
// without braces.

const mark = /\{\{([^{}\n]{1,40})\}\}/g;

/** plainNames takes the braces off every marked name. */
export function plainNames(text: string): string {
  return text.replace(mark, '$1');
}

/**
 * nameParts cuts text into plain strings and the AIs it marks. A marked
 * name that is not one of names stays as plain text, without its braces.
 */
export function nameParts(text: string, names: { has(name: string): boolean }): (string | { ai: string })[] {
  const out: (string | { ai: string })[] = [];
  let at = 0;
  let plain = '';
  for (const m of text.matchAll(mark)) {
    const i = m.index ?? 0;
    plain += text.slice(at, i);
    if (names.has(m[1])) {
      if (plain) out.push(plain);
      plain = '';
      out.push({ ai: m[1] });
    } else {
      plain += m[1];
    }
    at = i + m[0].length;
  }
  plain += text.slice(at);
  if (plain) out.push(plain);
  return out;
}
