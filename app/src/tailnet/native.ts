// The app's native Tailscale module (modules/tailnet, #57). Kept apart from
// node.ts so tests can put a stand-in here and nothing else changes.

import { requireOptionalNativeModule } from 'expo';

export type NativeFetchResult = { status: number; headers: string; body: string };

export type NativeTailnet = {
  /** Starts the node; it connects in the background. Again does nothing. */
  start(hostname: string): Promise<void>;
  stop(): Promise<void>;
  /** JSON: {state, authURL, name, owner, keyExpiry, ips, health}. */
  status(): Promise<string>;
  /** Asks Tailscale for a fresh login link; it shows up in status. */
  login(): Promise<void>;
  logout(): Promise<void>;
  /** JSON: {known, sameUser, online, name, ip} for a MagicDNS name. */
  peer(host: string): Promise<string>;
  fetch(method: string, url: string, headers: string, body: string, timeoutMs: number): Promise<NativeFetchResult>;
  recentLog(): Promise<string>;
};

let cached: NativeTailnet | null | undefined;

/** The module, or null in a build without it (Expo Go, iOS until it is written). */
export function nativeTailnet(): NativeTailnet | null {
  if (cached === undefined) cached = requireOptionalNativeModule<NativeTailnet>('AskorusTailnet');
  return cached && typeof cached.fetch === 'function' ? cached : null;
}
