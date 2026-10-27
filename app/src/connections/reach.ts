// Why a PC cannot be used right now, from what the phone can see (design:
// 오류 화면). The phone cannot look inside the tailnet, so the causes are
// ranked guesses, each with what to check.

import { AgentError, NetworkError } from '../api/client';

export type Cause =
  | 'unpaired' // the PC no longer knows this phone's token
  | 'wrong_account' // this phone's Tailscale account is not the PC owner's
  | 'agent_down' // Tailscale reached the PC but nothing answers behind serve
  | 'phone_offline' // failed at once: Tailscale off on the phone, or no network
  | 'pc_unreachable' // no answer in time: PC off, asleep, or Tailscale off there
  | 'other';

/** cause sorts a failed request into what the person should check first. */
export function cause(e: unknown): Cause {
  if (e instanceof AgentError) {
    if (e.code === 'unauthorized') return 'unpaired';
    if (e.code === 'wrong_account') return 'wrong_account';
    // tailscale serve answers itself when the agent behind it is gone.
    if (e.code === 'http_502' || e.code === 'http_503' || e.code === 'http_504') return 'agent_down';
    return 'other';
  }
  if (e instanceof NetworkError) return e.timedOut ? 'pc_unreachable' : 'phone_offline';
  return 'other';
}

/**
 * The causes worth checking, most likely first. A fast failure is most
 * often the phone's side, a slow one the PC's, but both stay listed.
 */
export function causesToCheck(c: Cause): Cause[] {
  switch (c) {
    case 'phone_offline':
      return ['phone_offline', 'pc_unreachable', 'agent_down'];
    case 'pc_unreachable':
      return ['pc_unreachable', 'phone_offline', 'agent_down'];
    default:
      return [c];
  }
}
