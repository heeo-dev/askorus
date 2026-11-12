// Where a link in an answer really goes (#47). An answer can carry words
// from a web page an AI read, so a link may show one address and lead to
// another: "[mybank.com](https://evil.example)". Only http(s) links open
// at all (Markdown's safeLink); this file tells the host a link leads to
// and whether the words shown name a different one.

/** linkHost is the host an http(s) address leads to, lower case and without "www.", or null. */
export function linkHost(href: string): string | null {
  // Up to the first "/", "?" or "#"; what stands before an "@" there is a
  // user name, not the host ("https://mybank.com@evil.example" goes to evil.example).
  const m = /^https?:\/\/([^/?#\\]*)/i.exec(href.trim());
  if (!m) return null;
  let host = m[1];
  const at = host.lastIndexOf('@');
  if (at >= 0) host = host.slice(at + 1);
  host = host.replace(/:\d*$/, '').replace(/\.$/, '').toLowerCase();
  if (host.startsWith('www.')) host = host.slice(4);
  return host || null;
}

// A host written in running text: labels with dots and an ending of
// letters ("example.com", "a.b.co.kr"), with or without http(s):// before.
const shownHost = /(?:https?:\/\/)?(?:[^\s/?#@]*@)?((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,})(?![a-z0-9-])/gi;

/**
 * shownElsewhere is the host the link's words name when it is not where the
 * link goes, or null. Words that name no address ("source", "출처") are
 * never a mismatch. A host and its subdomains count as the same place
 * ("example.com" shown, "docs.example.com" opened), and so does the other
 * way round.
 */
export function shownElsewhere(text: string, href: string): string | null {
  const real = linkHost(href);
  if (!real) return null;
  let other: string | null = null;
  for (const m of text.matchAll(shownHost)) {
    let shown = m[1].toLowerCase();
    if (shown.startsWith('www.')) shown = shown.slice(4);
    if (shown === real || real.endsWith('.' + shown) || shown.endsWith('.' + real)) return null;
    other ??= shown;
  }
  return other;
}
