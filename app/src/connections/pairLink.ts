// The pairing link the PC shows as a QR code:
//   askorus://pair?v=1&host=<MagicDNS name>&port=<serve HTTPS port>&code=<code>
// `askorus-agent pair -test-page` shows the test page instead:
//   https://<host>:<port>/test/#code=<code>
// Both are accepted, so the same PC window pairs the app or the browser.

export type PairLink = {
  host: string;
  port: number;
  code: string;
};

// A MagicDNS name or a Tailscale address; nothing that could carry a path,
// a user name or a different scheme.
const hostRe = /^[a-z0-9]([a-z0-9-]{0,62})(\.[a-z0-9]([a-z0-9-]{0,62}))*$/i;
const codeRe = /^[A-Z2-7]{16,64}$/;

export function parsePairLink(text: string): PairLink | null {
  let u: URL;
  try {
    u = new URL(text.trim());
  } catch {
    return null;
  }

  let host: string;
  let port: string;
  let code: string | null;
  if (u.protocol === 'askorus:') {
    // URL puts "pair" in host or pathname depending on the runtime.
    const what = (u.host || u.pathname.replace(/^\/+/, '')).toLowerCase();
    if (what !== 'pair') return null;
    const q = new URLSearchParams(u.search);
    if (q.get('v') !== '1') return null;
    host = q.get('host') ?? '';
    port = q.get('port') ?? '';
    code = q.get('code');
  } else if (u.protocol === 'https:') {
    if (!u.pathname.startsWith('/test')) return null;
    host = u.hostname;
    port = u.port || '443';
    code = new URLSearchParams(u.hash.replace(/^#/, '')).get('code');
  } else {
    return null;
  }

  const p = Number(port);
  code = (code ?? '').replace(/[-\s]/g, '').toUpperCase();
  if (!hostRe.test(host) || !Number.isInteger(p) || p < 1 || p > 65535 || !codeRe.test(code)) {
    return null;
  }
  return { host: host.toLowerCase(), port: p, code };
}
