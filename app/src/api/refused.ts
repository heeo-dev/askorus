// Tokens an agent refused (401 unauthorized): this phone was disconnected on
// that PC, and the token will not work again.
//
// The app remembers that while it runs and sends nothing more with such a
// token. Without this every screen that refreshes itself (the session list,
// the PC's info, the waiting pairing requests) went on asking, the agent
// counted each as a failed attempt, and after ten in ten minutes it shut the
// phone out of everything, pairing again included (found on a real phone,
// R-115). Pairing with that PC again gives a new token and clears the note.

const refused = new Set<string>();

const key = (base: string, token: string) => `${base}\n${token}`;

/** The agent's address as the client writes it (baseUrl in client.ts). */
export function agentBase(host: string, port: number): string {
  return `http://${host}:${port}`;
}

export function tokenRefused(base: string, token: string): boolean {
  return refused.has(key(base, token));
}

export function markRefused(base: string, token: string): void {
  refused.add(key(base, token));
}

/** A pairing with that agent was saved: whatever it refused before is forgotten. */
export function forgetRefused(base: string): void {
  for (const k of [...refused]) if (k.startsWith(`${base}\n`)) refused.delete(k);
}

/** For tests: nothing is remembered. */
export function resetRefused(): void {
  refused.clear();
}
