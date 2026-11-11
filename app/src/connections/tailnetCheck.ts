// Is this PC inside the person's own tailnet? (#42) Asked before the app
// sends a pairing code. A pairing link someone else sent can name a server
// under ts.net that Tailscale Funnel puts on the internet; that server would
// "allow" the pairing itself and then get the questions (R-94). A warning on
// the confirm step was the only guard. This asks the phone's network
// instead of the person:
//
// - a device of the tailnet resolves to a Tailscale address (100.64.0.0/10
//   or fd7a:115c:a1e0::/48), and with Tailscale on those addresses go into
//   the tailnet and nowhere else;
// - a name Funnel serves resolves to a public address, and a name of
//   another tailnet does not resolve at all;
// - the phone's own address on the route is a Tailscale one too, on a VPN
//   interface: with Tailscale off, a network that hands out 100.x addresses
//   and answers the name itself is not taken for the tailnet.
//
// The answer comes from the app's native module (modules/tailnet-check).
// Without it (Expo Go, iOS until it is written, tests) the check is skipped
// and pairing goes on as before. A device shared into the tailnet by
// someone else has a Tailscale address and passes: the confirm step's
// warning still stands.

import { requireOptionalNativeModule } from 'expo';

export type Route = { address: string; local: string; via: string };
export type Lookup = (host: string, port: number) => Promise<Route[]>;

/**
 * tailnet: every address is a Tailscale one, reached through Tailscale.
 * outside: an address or a route that is not Tailscale's. No code is sent.
 * unresolved: the name has no address (Tailscale off, a wrong name). No code is sent.
 * unknown: this build cannot tell; pairing goes on as before.
 */
export type TailnetAnswer = 'tailnet' | 'outside' | 'unresolved' | 'unknown';

type TailnetModule = { lookup?: Lookup };

let native: TailnetModule | null | undefined;

function nativeLookup(): Lookup | null {
  if (native === undefined) native = requireOptionalNativeModule<TailnetModule>('AskorusTailnetCheck');
  return native && typeof native.lookup === 'function' ? native.lookup.bind(native) : null;
}

function ipv4(text: string): number[] | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(text);
  if (!m) return null;
  const parts = m.slice(1).map(Number);
  return parts.every((p) => p <= 255) ? parts : null;
}

/** The eight 16-bit groups of an IPv6 address, or null. */
function ipv6(text: string): number[] | null {
  if (!/^[0-9a-f:]+$/i.test(text) || !text.includes(':')) return null;
  const halves = text.split('::');
  if (halves.length > 2) return null;
  const groups = (part: string) => (part === '' ? [] : part.split(':'));
  const head = groups(halves[0]);
  const tail = halves.length === 2 ? groups(halves[1]) : [];
  const all = [...head, ...tail];
  if (all.some((g) => !/^[0-9a-f]{1,4}$/i.test(g))) return null;
  if (halves.length === 1) return all.length === 8 ? all.map((g) => parseInt(g, 16)) : null;
  if (all.length > 7) return null;
  const zeros = new Array<number>(8 - all.length).fill(0);
  return [...head.map((g) => parseInt(g, 16)), ...zeros, ...tail.map((g) => parseInt(g, 16))];
}

/** A Tailscale address: 100.64.0.0/10 or fd7a:115c:a1e0::/48. */
export function isTailscaleAddress(address: string): boolean {
  const text = address.trim().split('%')[0];
  const v4 = ipv4(text);
  if (v4) return v4[0] === 100 && v4[1] >= 64 && v4[1] <= 127;
  const v6 = ipv6(text);
  return !!v6 && v6[0] === 0xfd7a && v6[1] === 0x115c && v6[2] === 0xa1e0;
}

// Android names the interface of a VPN app tun0, tun1, …
const vpnInterface = /^tun\d+$/;

/** What a lookup's answer means. An empty local or via is "not known" and decides nothing. */
export function judgeRoutes(routes: Route[]): TailnetAnswer {
  if (!routes.length) return 'unresolved';
  for (const r of routes) {
    if (!isTailscaleAddress(r.address)) return 'outside';
    if (r.local && !isTailscaleAddress(r.local)) return 'outside';
    if (r.via && !vpnInterface.test(r.via)) return 'outside';
  }
  return 'tailnet';
}

const lookupTimeoutMs = 10_000;

/**
 * checkTailnetHost asks the phone's network where the PC's name leads.
 * The lookup is the native module's unless one is given (tests).
 */
export async function checkTailnetHost(host: string, port: number, lookup: Lookup | null = nativeLookup(), timeoutMs = lookupTimeoutMs): Promise<TailnetAnswer> {
  if (!lookup) return 'unknown';
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const routes = await Promise.race([
      lookup(host, port),
      new Promise<null>((resolve) => {
        timer = setTimeout(() => resolve(null), timeoutMs);
      }),
    ]);
    // No answer in time reads as no address: nothing is sent, and the person can try again.
    if (routes === null) return 'unresolved';
    if (!Array.isArray(routes)) return 'unknown';
    return judgeRoutes(routes.map((r) => ({ address: String(r?.address ?? ''), local: String(r?.local ?? ''), via: String(r?.via ?? '') })));
  } catch (e) {
    // The module failed in a way the link cannot cause (the name is a plain
    // ts.net name by now): the check is skipped, not held against the person.
    console.warn('[tailnet check]', e);
    return 'unknown';
  } finally {
    if (timer) clearTimeout(timer);
  }
}
