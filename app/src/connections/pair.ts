// Pairing with a PC: send the QR code, wait for the person at the PC,
// then save the connection. Kept apart from the screen so it can be tested.

import { AgentClient, type WaitOptions, baseUrl } from '../api/client';
import type { PairLink } from './pairLink';
import { type Connection, ConnectionStore } from './store';

export type PairDeps = {
  store: ConnectionStore;
  fetch?: typeof fetch;
  sleep?: (ms: number, signal?: AbortSignal) => Promise<void>;
  now?: () => Date;
};

// Control, format and separator characters the agent's printable check refuses.
const nonPrintable: Array<[number, number]> = [
  [0x00, 0x1f],
  [0x7f, 0x9f],
  [0xad, 0xad],
  [0x200b, 0x200f],
  [0x2028, 0x202e],
  [0x2060, 0x206f],
  [0xfeff, 0xfeff],
];

const printable = (ch: string) => {
  const c = ch.codePointAt(0) ?? 0;
  return !nonPrintable.some(([lo, hi]) => c >= lo && c <= hi);
};

/** cleanDeviceName keeps what the agent accepts: 1–64 printable characters. */
export function cleanDeviceName(name: string | null | undefined, fallback = 'Phone'): string {
  const kept = Array.from(name ?? '')
    .filter(printable)
    .join('')
    .trim();
  const s = Array.from(kept).slice(0, 64).join('').trim();
  return s || fallback;
}

export async function pairWithPC(
  link: PairLink,
  deviceName: string,
  deps: PairDeps,
  wait: WaitOptions = {},
): Promise<Connection> {
  const client = new AgentClient(baseUrl(link.host, link.port), { fetch: deps.fetch, sleep: deps.sleep });
  const paired = await client.pair(link.code, cleanDeviceName(deviceName), wait);

  // The PC's own name for the list. Pairing already worked, so a failure
  // here only means the host name is shown instead.
  let name = link.host.split('.')[0];
  try {
    const info = await client.withToken(paired.token).info(wait.signal);
    if (info.hostname) name = info.hostname;
  } catch {
    // keep the host name
  }

  return deps.store.add(
    {
      kind: 'pc',
      name,
      host: link.host,
      port: link.port,
      deviceId: paired.deviceId,
      deviceName: paired.deviceName,
      pairedAt: (deps.now?.() ?? new Date()).toISOString(),
    },
    paired.token,
  );
}
