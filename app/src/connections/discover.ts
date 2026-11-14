// Finding the person's PCs without a QR code (#58). With Tailscale built in,
// the phone and the PC are signed in to the same account, so the app's own
// node already knows the account's devices. The agent names its device
// "askorus-<pc>", and answers a short hello to a device of its owner's
// account before any pairing. A QR code is only needed for a PC that does
// not show up here (another port, a device name that was changed).

import { AgentClient, baseUrl } from '@/api/client';
import { ownDevices } from '@/tailnet/node';

import type { Connection } from './store';

/**
 * The port an agent listens on inside the tailnet unless its settings say
 * otherwise. The agent's device is its own, so nothing else can hold the
 * port; a PC set to another one is paired by its QR code.
 */
export const AGENT_PORT = 8790;

export type FoundPC = {
  host: string;
  port: number;
  /** The PC's own name. */
  name: string;
  os?: string;
  /** A pairing window is open on the PC: it takes this phone without asking. */
  window: boolean;
  /** This phone is paired with it already. */
  paired: boolean;
};

/** findPCs looks among the account's devices for agents that answer. */
export async function findPCs(paired: Connection[], deps: { fetch?: typeof fetch } = {}): Promise<FoundPC[]> {
  const devices = (await ownDevices()).filter((d) => d.online && d.host.toLowerCase().startsWith('askorus-'));
  const found = await Promise.all(
    devices.map(async (d): Promise<FoundPC | null> => {
      try {
        const h = await new AgentClient(baseUrl(d.name, AGENT_PORT), { fetch: deps.fetch, timeoutMs: 4000 }).hello();
        if (h.name !== 'askorus-agent') return null;
        return {
          host: d.name,
          port: AGENT_PORT,
          name: h.hostname || d.host,
          os: h.os,
          window: h.pairing === 'window',
          paired: paired.some((c) => c.host === d.name && c.port === AGENT_PORT),
        };
      } catch {
        return null; // another phone of the account, or a PC whose agent does not answer
      }
    }),
  );
  return found.filter((x): x is FoundPC => !!x);
}

/** newMatch is the two digits the phone shows while it asks, and the PC shows where it asks or reports. */
export function newMatch(random: () => number = Math.random): string {
  return String(10 + Math.floor(random() * 90));
}
