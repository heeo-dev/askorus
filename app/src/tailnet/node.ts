// The app's own device on the person's Tailscale network (#57). Tailscale
// is inside the app: nothing to install, no VPN to turn on, and other apps
// (a payment app that refuses to work under a VPN) see nothing of it. The
// person signs in once, in the browser, with the account the PC uses.
//
// Everything the app sends to a PC goes through tailnetFetch. The native
// side sends it only to a device of the person's own Tailscale account
// (tsbridge.Fetch), so no screen has to remember to check.
//
// The node is started only after the person chose to sign in (the flag
// below): until then the app talks to no server at all.

import * as Device from 'expo-device';
import { useEffect, useSyncExternalStore } from 'react';
import { AppState } from 'react-native';

import { type KeyValue, secureKeyValue } from '@/connections/store';

import { type NativeTailnet, nativeTailnet } from './native';

/**
 * unavailable: this build has no Tailscale inside (Expo Go, iOS for now).
 * off: the person has not signed in on this phone (the node is not started).
 * starting: the node is coming up, or has just been asked for a login link.
 * needsLogin: open loginURL in the browser and sign in.
 * needsApproval: signed in, but the tailnet's admin has to approve the device.
 * running: PCs of the same account can be reached.
 */
export type TailnetState = 'unavailable' | 'off' | 'starting' | 'needsLogin' | 'needsApproval' | 'running';

export type TailnetStatus = {
  state: TailnetState;
  loginURL?: string;
  /** This phone's name in the tailnet. */
  name?: string;
  /** The account it is signed in as. */
  owner?: string;
  /** When the sign-in ends (RFC 3339); absent when it never does. */
  keyExpiry?: string;
};

/** Codes of TailnetError, as the native side names them (tsbridge). */
export type TailnetCode =
  | 'tailnet_off'
  | 'tailnet_login'
  | 'tailnet_not_own'
  | 'tailnet_unknown_host'
  | 'tailnet_not_connected'
  | 'tailnet_bad_request'
  | 'tailnet_too_large'
  | 'tailnet_network';

export class TailnetError extends Error {
  constructor(
    readonly code: TailnetCode | string,
    message = code,
  ) {
    super(message);
    this.name = 'TailnetError';
  }
}

const onKey = 'tailnet.on.v1';

let kv: KeyValue = secureKeyValue;
let starting: Promise<void> | null = null;
let started = false;
let current: TailnetStatus = { state: nativeTailnet() ? 'off' : 'unavailable' };
const listeners = new Set<() => void>();

function publish(next: TailnetStatus) {
  if (JSON.stringify(next) === JSON.stringify(current)) return;
  current = next;
  listeners.forEach((l) => l());
}

/** For tests: forgets everything this module holds in memory. */
export function resetTailnet(store: KeyValue = secureKeyValue) {
  kv = store;
  starting = null;
  started = false;
  current = { state: nativeTailnet() ? 'off' : 'unavailable' };
}

export function tailnetAvailable(): boolean {
  return nativeTailnet() !== null;
}

/**
 * This phone's device name in the tailnet: "askorus-" and what a DNS label
 * may hold of the phone's model. Not the name the person gave the phone: that
 * name ("Sam's phone") can hold a person's name, and this one goes to
 * Tailscale's servers. The name the PC lists stays the person's to choose
 * (pairing), and that one goes only to their own PC.
 */
export function tailnetHostName(device: string | null | undefined = Device.modelName): string {
  const label = (device ?? '')
    .toLowerCase()
    .replace(/[\s._]+/g, '-')
    .replace(/[^a-z0-9-]/g, '')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '');
  return `askorus-${label || 'phone'}`.slice(0, 63).replace(/-+$/, '');
}

async function enabled(): Promise<boolean> {
  return (await kv.get(onKey).catch(() => null)) === '1';
}

function coded(e: unknown): TailnetError {
  if (e instanceof TailnetError) return e;
  const code = (e as { code?: unknown } | null)?.code;
  const message = e instanceof Error ? e.message : String(e);
  return new TailnetError(typeof code === 'string' && code.startsWith('tailnet_') ? code : 'tailnet_network', message);
}

async function start(native: NativeTailnet): Promise<void> {
  if (started) return;
  starting ??= native
    .start(tailnetHostName())
    .then(() => {
      started = true;
    })
    .finally(() => {
      starting = null;
    });
  await starting;
}

/**
 * resumeTailnet starts the node when the person signed in on this phone
 * before. The root layout calls it once at launch.
 */
export async function resumeTailnet(): Promise<void> {
  const native = nativeTailnet();
  if (!native || !(await enabled())) return;
  try {
    await start(native);
  } catch {
    // The status says what is wrong when a screen asks.
  }
  await readTailnet();
}

function parseStatus(json: string): TailnetStatus {
  let v: { state?: string; authURL?: string; name?: string; owner?: string; keyExpiry?: string };
  try {
    v = JSON.parse(json);
  } catch {
    return { state: 'starting' };
  }
  const rest = {
    ...(v.name ? { name: v.name } : {}),
    ...(v.owner ? { owner: v.owner } : {}),
    ...(v.keyExpiry ? { keyExpiry: v.keyExpiry } : {}),
  };
  switch (v.state) {
    case 'Running':
      return { state: 'running', ...rest };
    case 'NeedsLogin':
      // The link is there a moment after the node is asked for one
      // (loginTailnet); a sign-in that ran out has none until then.
      return v.authURL ? { state: 'needsLogin', loginURL: v.authURL } : { state: 'needsLogin' };
    case 'NeedsMachineAuth':
      return { state: 'needsApproval', ...rest };
    case 'Off':
      return { state: 'off' };
    default:
      return { state: 'starting', ...rest };
  }
}

/** readTailnet asks the node for its state now. */
export async function readTailnet(): Promise<TailnetStatus> {
  const native = nativeTailnet();
  if (!native) {
    publish({ state: 'unavailable' });
    return current;
  }
  if (!started) {
    // Signed in on this phone before: the node is started here if the
    // launch has not done it yet. Never signed in: it stays off.
    if (!(await enabled())) {
      publish({ state: 'off' });
      return current;
    }
    try {
      await start(native);
    } catch {
      publish({ state: 'off' });
      return current;
    }
  }
  try {
    publish(parseStatus(await native.status()));
  } catch {
    // Keep what was known: one failed read is not a change of state.
  }
  return current;
}

/**
 * loginTailnet starts the node (from now on at every launch too) and asks
 * for a login link. The link arrives in the status a moment later; the
 * screen opens it in the browser.
 */
export async function loginTailnet(): Promise<void> {
  const native = nativeTailnet();
  if (!native) throw new TailnetError('tailnet_off');
  await kv.set(onKey, '1');
  publish({ state: 'starting' });
  try {
    await start(native);
    const st = await readTailnet();
    // A first start asks Tailscale for a link by itself; a sign-in that ran
    // out, or one the person ended, has none until it is asked for.
    if (st.state !== 'running' && !(st.state === 'needsLogin' && st.loginURL)) await native.login();
  } catch (e) {
    throw coded(e);
  }
  await readTailnet();
}

/**
 * logoutTailnet removes this phone from the tailnet and forgets its key.
 * The paired PCs stay in the list; they cannot be reached until the person
 * signs in again.
 */
export async function logoutTailnet(): Promise<void> {
  const native = nativeTailnet();
  await kv.remove(onKey).catch(() => undefined);
  if (native && started) {
    try {
      await native.logout();
    } finally {
      await native.stop().catch(() => undefined);
      started = false;
    }
  }
  publish({ state: native ? 'off' : 'unavailable' });
}

/**
 * What the tailnet says about a PC's name, before a pairing code is sent
 * (#42): one of the person's own devices, someone else's (shared in, or a
 * server), or not there at all. 'unavailable' is a build without Tailscale
 * inside, where nothing can be sent anyway.
 */
export type PeerAnswer = 'own' | 'other' | 'unknown' | 'unavailable';

export async function ownDevice(host: string): Promise<PeerAnswer> {
  const native = nativeTailnet();
  if (!native) return 'unavailable';
  if (!started && !(await enabled())) throw new TailnetError('tailnet_login');
  try {
    await start(native);
    const p = JSON.parse(await native.peer(host)) as { known?: boolean; sameUser?: boolean };
    if (!p.known) return 'unknown';
    return p.sameUser ? 'own' : 'other';
  } catch (e) {
    throw coded(e);
  }
}

/** One other device of the person's own Tailscale account. */
export type OwnDevice = { name: string; host: string; online: boolean; os?: string };

/**
 * The other devices of the account this phone is signed in as: where the
 * app looks for the person's PCs, so pairing needs no QR code to say where
 * the PC is (#58). Only these devices can get a request at all.
 */
export async function ownDevices(): Promise<OwnDevice[]> {
  const native = nativeTailnet();
  if (!native) return [];
  if (!started && !(await enabled())) throw new TailnetError('tailnet_login');
  try {
    await start(native);
    const list = JSON.parse(await native.peers()) as OwnDevice[];
    return Array.isArray(list) ? list : [];
  } catch (e) {
    throw coded(e);
  }
}

// A request the client gave up on (its own timeout, or the person leaving)
// rejects at once; the native call ends on its own a little later.
const nativeTimeoutMs = 90_000;

function aborted(): Error {
  const e = new Error('Aborted');
  e.name = 'AbortError';
  return e;
}

/**
 * tailnetFetch is fetch through the app's node. It takes what AgentClient
 * sends (a URL, a method, string headers, a string body, a signal) and
 * gives back what AgentClient reads (ok, status, json, text).
 */
export const tailnetFetch: typeof fetch = async (input, init) => {
  const native = nativeTailnet();
  if (!native) throw new TailnetError('tailnet_off');
  if (!started && !(await enabled())) throw new TailnetError('tailnet_login');
  const signal = init?.signal ?? undefined;
  if (signal?.aborted) throw aborted();

  // AgentClient sends a plain object of headers; it goes on as it is.
  const headers: Record<string, string> = {};
  for (const [key, value] of Object.entries((init?.headers ?? {}) as Record<string, string>)) {
    if (typeof value === 'string') headers[key] = value;
  }
  const body = typeof init?.body === 'string' ? init.body : '';

  let onAbort: (() => void) | undefined;
  const gaveUp = new Promise<never>((_, reject) => {
    onAbort = () => reject(aborted());
    signal?.addEventListener('abort', onAbort, { once: true });
  });
  try {
    await Promise.race([start(native), gaveUp]);
    const res = await Promise.race([native.fetch(init?.method ?? 'GET', String(input), JSON.stringify(headers), body, nativeTimeoutMs), gaveUp]);
    return answer(res.status, res.body, res.headers);
  } catch (e) {
    if ((e as Error)?.name === 'AbortError') throw e;
    throw coded(e);
  } finally {
    if (onAbort) signal?.removeEventListener('abort', onAbort);
  }
};

function answer(status: number, body: string, headersJSON: string): Response {
  let headers: Record<string, string> = {};
  try {
    headers = JSON.parse(headersJSON || '{}');
  } catch {
    // no headers
  }
  const lower = Object.fromEntries(Object.entries(headers).map(([k, v]) => [k.toLowerCase(), String(v)]));
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: (name: string) => lower[name.toLowerCase()] ?? null },
    json: async () => JSON.parse(body),
    text: async () => body,
  } as unknown as Response;
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/**
 * useTailnet follows the node's state while a screen shows it: read at
 * once, then every second while the app is in front (signing in happens in
 * the browser, and the app must notice when the person comes back).
 */
export function useTailnet(pollMs = 1000): TailnetStatus {
  const status = useSyncExternalStore(
    subscribe,
    () => current,
    () => current,
  );
  useEffect(() => {
    let live = true;
    // Not while the app is in the background: nobody looks, and the phone
    // freezes the app there anyway.
    const read = () => {
      if (live && AppState.currentState !== 'background') readTailnet().catch(() => undefined);
    };
    read();
    const timer = setInterval(read, pollMs);
    const sub = AppState.addEventListener('change', read);
    return () => {
      live = false;
      clearInterval(timer);
      sub.remove();
    };
  }, [pollMs]);
  return status;
}
