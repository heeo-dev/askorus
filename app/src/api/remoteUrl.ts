// The link of a session's Remote Control page, which the PC sends and the
// app opens in the Claude app or the browser. The agent only ever sends
// https://claude.ai/code/session_… (its isRemoteURL); the app checks the
// same shape again before opening it, so a server that is not the agent
// cannot make the app open another app's link, a phone number or another
// site (R-94).

import { Linking } from 'react-native';

const remoteRe = /^https:\/\/claude\.ai\/code\/session_[A-Za-z0-9_-]{1,128}$/;

/** isRemoteUrl tells whether url is a Remote Control session link. */
export function isRemoteUrl(url: string | null | undefined): url is string {
  return typeof url === 'string' && remoteRe.test(url);
}

/** openRemoteUrl opens a Remote Control session link and refuses anything else. */
export async function openRemoteUrl(url: string | null | undefined): Promise<void> {
  if (!isRemoteUrl(url)) throw new Error('bad_remote_url');
  await Linking.openURL(url);
}
