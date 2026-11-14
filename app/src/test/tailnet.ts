// The tests' handle on the stand-in for the app's Tailscale node (see
// setup.ts): what state it is in, whose device a PC name is, and what the
// app asked of it.

type MockState = {
  available: boolean;
  status: Record<string, unknown>;
  peers: Record<string, { known: boolean; sameUser: boolean }>;
  devices: Array<{ name: string; host: string; online: boolean; os?: string }>;
  afterLogin: Record<string, unknown> | null;
  calls: string[];
};

export const mockTailnet: MockState = (jest.requireMock('@/tailnet/native') as { mockTailnetState: MockState }).mockTailnetState;

export const signedIn = { state: 'Running', name: 'askorus-test-phone.tail0.ts.net', owner: 'me@example.com' };
export const LOGIN_URL = 'https://login.tailscale.com/a/abc123';

/** Back to a phone that is signed in and sees every PC as its own. */
export function resetMockTailnet() {
  mockTailnet.available = true;
  mockTailnet.status = { ...signedIn };
  mockTailnet.peers = {};
  mockTailnet.devices = [];
  mockTailnet.afterLogin = null;
  mockTailnet.calls = [];
}

/**
 * A phone where the person has never signed in: the node is not started,
 * and pressing the sign-in button brings the link.
 */
export async function neverSignedIn() {
  mockTailnet.status = { state: 'NeedsLogin' };
  mockTailnet.afterLogin = { state: 'NeedsLogin', authURL: LOGIN_URL };
  await (jest.requireMock('expo-secure-store') as { deleteItemAsync(key: string): Promise<void> }).deleteItemAsync('tailnet.on.v1');
  (jest.requireActual('@/tailnet/node') as { resetTailnet(): void }).resetTailnet();
}

/** The person finished signing in in the browser. */
export function finishSignIn() {
  mockTailnet.status = { ...signedIn };
}
