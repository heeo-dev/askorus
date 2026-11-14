// The pairing link the PC shows as a QR code:
//   askorus://pair?v=2&host=<the agent's name in the tailnet>&port=<port>&code=<code>
// Version 2 (#57): the host is the PC agent's own Tailscale node, which the
// app reaches through its own node. A version 1 link came from an agent
// behind `tailscale serve`; that way is gone, and the app says to update
// the agent instead of trying it.

export type PairLink = {
  host: string;
  port: number;
  code: string;
};

// The agent's MagicDNS name: a name under ts.net and nothing else (R-94). A
// link someone sends could name any server; whether the name is one of the
// person's own devices is then asked of the tailnet itself before anything
// is sent (tailnet/node ownDevice, and again on every request). Nothing
// that could carry a path, a user name or a different scheme either.
const hostRe = /^[a-z0-9]([a-z0-9-]{0,62})(\.[a-z0-9]([a-z0-9-]{0,62}))*\.ts\.net$/i;
const codeRe = /^[A-Z2-7]{16,64}$/;

function pairQuery(text: string): URLSearchParams | null {
  let u: URL;
  try {
    u = new URL(text.trim());
  } catch {
    return null;
  }
  if (u.protocol !== 'askorus:') return null;
  // URL puts "pair" in host or pathname depending on the runtime.
  const what = (u.host || u.pathname.replace(/^\/+/, '')).toLowerCase();
  return what === 'pair' ? new URLSearchParams(u.search) : null;
}

export function parsePairLink(text: string): PairLink | null {
  const q = pairQuery(text);
  if (!q || q.get('v') !== '2') return null;
  const host = q.get('host') ?? '';
  const p = Number(q.get('port') ?? '');
  const code = (q.get('code') ?? '').replace(/[-\s]/g, '').toUpperCase();
  if (!hostRe.test(host) || !Number.isInteger(p) || p < 1 || p > 65535 || !codeRe.test(code)) {
    return null;
  }
  return { host: host.toLowerCase(), port: p, code };
}

/** A pairing link of an agent older than 0.3 (version 1): the PC's agent has to be updated. */
export function isOldPairLink(text: string): boolean {
  return pairQuery(text)?.get('v') === '1';
}
