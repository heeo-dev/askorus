// Names for screenshots. A build made with EXPO_PUBLIC_ASKORUS_DEMO=1 shows
// example names in place of the person's own Tailscale account and PC, so a
// picture of the pairing screens can be published. Only what is drawn
// changes: the app still talks to the real PC under its real name. Release
// builds are made without the variable, and then nothing here does anything.

const on = process.env.EXPO_PUBLIC_ASKORUS_DEMO === '1';

/** The Tailscale account as shown. */
export const shownAccount = (account: string | undefined) => (on && account ? 'you@example.com' : account);

/** A PC's name as shown. */
export const shownPC = (name: string) => (on ? 'MY-PC' : name);

/** The first label of a PC's Tailscale name as shown. */
export const shownHost = (label: string) => (on ? 'askorus-my-pc' : label);
