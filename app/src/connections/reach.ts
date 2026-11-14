// Why a PC cannot be used right now, from what the phone can see (design:
// 오류 화면). The app's own Tailscale node knows some causes for certain
// (the phone is not signed in, the PC is not one of the person's devices);
// the rest are ranked guesses, each with what to check.

import { AgentError, NetworkError } from '../api/client';
import { TailnetError } from '../tailnet/node';

export type Cause =
  | 'unpaired' // the PC no longer knows this phone's token
  | 'wrong_account' // this phone's Tailscale account is not the PC owner's
  | 'tailnet_login' // the app is not signed in to Tailscale on this phone
  | 'not_own_pc' // the PC is not (or no longer) a device of this phone's Tailscale account
  | 'phone_offline' // failed at once: no network on the phone
  | 'pc_unreachable' // no answer in time: PC off or asleep, or its agent stopped or signed out
  | 'other';

/** cause sorts a failed request into what the person should check first. */
export function cause(e: unknown): Cause {
  if (e instanceof AgentError) {
    if (e.code === 'unauthorized') return 'unpaired';
    if (e.code === 'wrong_account') return 'wrong_account';
    return 'other';
  }
  if (e instanceof NetworkError) {
    const why = e.cause instanceof TailnetError ? e.cause.code : '';
    if (why === 'tailnet_login' || why === 'tailnet_off') return 'tailnet_login';
    if (why === 'tailnet_not_own' || why === 'tailnet_unknown_host') return 'not_own_pc';
    return e.timedOut ? 'pc_unreachable' : 'phone_offline';
  }
  return 'other';
}

/**
 * The causes worth checking, most likely first. A fast failure is most
 * often the phone's side, a slow one the PC's, but both stay listed.
 */
export function causesToCheck(c: Cause): Cause[] {
  switch (c) {
    case 'phone_offline':
      return ['phone_offline', 'pc_unreachable'];
    case 'pc_unreachable':
      return ['pc_unreachable', 'phone_offline'];
    default:
      return [c];
  }
}
