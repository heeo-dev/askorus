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
  /** The other devices of the person's own account, as JSON (#58). */
  peers(): Promise<string>;
  /** id names the request for cancel (#76). */
  fetch(id: string, method: string, url: string, headers: string, body: string, timeoutMs: number): Promise<NativeFetchResult>;
  /**
   * fetch with a file of the app's own as the body (#14: a photo that goes
   * with a question). path is a file:// address in the app's cache or files.
   * A build from before it has none.
   */
  sendFile?(id: string, method: string, url: string, headers: string, path: string, timeoutMs: number): Promise<NativeFetchResult>;
  /**
   * fetch whose 2xx answer goes into path, a new file of the app's own (#14:
   * a picture an AI made); body is then empty. A build from before has none.
   */
  fetchFile?(id: string, method: string, url: string, headers: string, path: string, timeoutMs: number): Promise<NativeFetchResult>;
  recentLog(): Promise<string>;
  /**
   * Ends the request with that id when it still runs: the app gave up on it
   * (#76). A build from before has none.
   */
  cancel?(id: string): Promise<void>;
};

let cached: NativeTailnet | null | undefined;

/** The module, or null in a build without it (Expo Go, iOS until it is written). */
export function nativeTailnet(): NativeTailnet | null {
  if (cached === undefined) cached = requireOptionalNativeModule<NativeTailnet>('AskorusTailnet');
  return cached && typeof cached.fetch === 'function' ? cached : null;
}
