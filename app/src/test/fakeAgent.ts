// A pretend PC agent for screen tests. It answers the /v1 routes the app
// uses, keeps a little state, and records what it was asked.

import type { Folder, Session } from '@/api/types';

export const HOST = 'desktop-test.tail0.ts.net';
export const PORT = 8790;
export const CODE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
export const TOKEN = 'device-token';
export const PAIR_LINK = `asksemble://pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}`;

export type Call = { method: string; path: string; query: URLSearchParams; body?: any; auth?: string };

export function session(over: Partial<Session> & { id: string }): Session {
  return {
    agent: 'claude',
    title: over.id,
    lastActive: new Date().toISOString(),
    state: 'stopped',
    openModes: ['official'],
    ...over,
  };
}

export class FakeAgent {
  sessions: Session[] = [];
  folders: Folder[] = [];
  calls: Call[] = [];
  /** What the PC answers to pairing: approve, deny, or keep waiting. */
  pairAnswer: 'approve' | 'deny' | 'wait' = 'approve';
  /** When false, every token is refused, as after `devices revoke`. */
  tokenValid = true;
  remoteUrl = 'https://claude.ai/code/session_test';
  /** Codes /v1/info reports for Claude, such as claude_service_elevated. */
  notices: string[] = [];
  /**
   * Like the real agent, the PUT answer still shows the old server state;
   * the new one appears on the next GET /v1/folders.
   */
  serverLag = true;
  private settle: Array<() => void> = [];
  /** How the PC can be reached: fine, agent stopped behind serve, or not at all. */
  reach: 'ok' | 'agent_down' | 'offline' = 'ok';
  private jobs = new Map<string, any>();
  private n = 0;

  fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input));
    const headers = (init?.headers ?? {}) as Record<string, string>;
    const call: Call = {
      method: init?.method ?? 'GET',
      path: url.pathname,
      query: url.searchParams,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
      auth: headers.Authorization,
    };
    this.calls.push(call);
    if (this.reach === 'offline' || url.host !== `${HOST}:${PORT}`) throw new TypeError('Network request failed');
    // tailscale serve answers by itself when nothing listens behind it.
    if (this.reach === 'agent_down') return new Response('<html>502 Bad Gateway</html>', { status: 502 });
    return this.route(call);
  }) as typeof fetch;

  callsTo(method: string, path: string) {
    return this.calls.filter((c) => c.method === method && c.path === path);
  }

  private route(c: Call): Response {
    const key = `${c.method} ${c.path}`;
    if (key === 'POST /v1/pair') {
      if (c.body?.code !== CODE) return err(403, 'pair_bad_code');
      return json(202, { requestId: 'req-1', status: 'pending', pollAfterMs: 1 });
    }
    if (key === 'POST /v1/pair/status') {
      if (this.pairAnswer === 'deny') return err(403, 'pair_denied');
      if (this.pairAnswer === 'wait') return json(200, { status: 'pending', pollAfterMs: 1, apiVersion: 1 });
      return json(200, {
        status: 'approved',
        token: TOKEN,
        deviceId: 'dev-1',
        deviceName: c.body ? 'Test Phone' : '',
        apiVersion: 1,
      });
    }

    if (!this.tokenValid || c.auth !== `Bearer ${TOKEN}`) return err(401, 'unauthorized');

    if (key === 'GET /v1/info') {
      return json(200, {
        name: 'asksemble-agent',
        version: '0.1.0',
        apiVersion: 1,
        hostname: 'DESKTOP-TEST',
        os: 'windows',
        agents: [{ agent: 'claude', version: '2.1.310', available: true, capabilities: [], notices: this.notices }],
        capabilities: ['list', 'start', 'resume', 'stop', 'remove', 'open_official', 'serve'],
        reserved: [],
      });
    }
    if (key === 'DELETE /v1/devices/self') {
      this.tokenValid = false;
      return json(200, { deviceId: 'dev-1', removed: true });
    }
    if (key === 'GET /v1/folders') {
      this.settle.splice(0).forEach((apply) => apply()); // by the next ask the server caught up
      return json(200, { folders: this.folders });
    }
    const fs = c.path.match(/^\/v1\/folders\/([^/]+)\/server$/);
    if (fs && c.method === 'PUT') {
      const id = decodeURIComponent(fs[1]);
      const f = this.folders.find((x) => x.id === id);
      if (!f) return err(404, 'folder_not_found');
      const enabled = !!c.body?.enabled;
      if (f.source === 'history') {
        if (!enabled) return json(200, f);
        // Keeping a history folder open saves it to the allowed list.
        const saved = {
          ...f,
          id: f.name,
          source: 'config',
          lastActive: undefined,
          server: { ...f.server, enabled: true },
          status: { state: 'running', sessions: 0, capacity: f.server.capacity },
        };
        this.folders = [...this.folders.filter((x) => x.id !== id && x.source !== 'history'), saved, ...this.folders.filter((x) => x.source === 'history' && x.id !== id)];
        return json(200, saved);
      }
      const next = {
        ...f,
        server: { ...f.server, enabled },
        status: enabled ? { state: 'running', sessions: 0, capacity: f.server.capacity } : { state: 'off', sessions: 0 },
      };
      if (!this.serverLag) {
        this.folders = this.folders.map((x) => (x.id === id ? next : x));
        return json(200, next);
      }
      // The setting is saved at once; the server catches up later.
      const saved = { ...f, server: next.server };
      this.folders = this.folders.map((x) => (x.id === id ? saved : x));
      this.settle.push(() => {
        this.folders = this.folders.map((x) => (x.id === id ? next : x));
      });
      return json(200, saved);
    }
    if (key === 'POST /v1/sessions') {
      const f = this.folders.find((x) => x.id === c.body?.folder);
      if (!f) return err(404, 'folder_not_found');
      const jobId = `job-${++this.n}`;
      const s = session({ id: `claude:new-${this.n}`, title: c.body.name || f.name, state: 'running', folder: f.path });
      this.sessions = [s, ...this.sessions];
      this.jobs.set(jobId, { action: 'started', remoteUrl: this.remoteUrl, session: s });
      return json(202, { jobId, kind: 'start', status: 'running', pollAfterMs: 1 });
    }
    if (key === 'GET /v1/sessions') {
      const q = (c.query.get('q') ?? '').toLowerCase();
      const states = c.query.get('state')?.split(',');
      const list = this.sessions.filter(
        (s) => (!q || s.title.toLowerCase().includes(q)) && (!states || states.includes(s.state)),
      );
      return json(200, { sessions: list, warnings: [] });
    }

    const m = c.path.match(/^\/v1\/sessions\/([^/]+)(?:\/(open|stop))?$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      const s = this.sessions.find((x) => x.id === id);
      if (!s) return err(404, 'session_not_found');
      if (c.method === 'POST' && m[2] === 'open') {
        const jobId = `job-${++this.n}`;
        const running = { ...s, state: 'running' as const, remoteUrl: this.remoteUrl };
        this.jobs.set(jobId, { action: 'resumed', remoteUrl: this.remoteUrl, session: running });
        this.sessions = this.sessions.map((x) => (x.id === id ? running : x));
        return json(202, { jobId, kind: 'open', status: 'running', pollAfterMs: 1 });
      }
      if (c.method === 'POST' && m[2] === 'stop') {
        const stopped = { ...s, state: 'stopped' as const };
        this.sessions = this.sessions.map((x) => (x.id === id ? stopped : x));
        return json(200, { session: stopped });
      }
      if (c.method === 'DELETE' && !m[2]) {
        this.sessions = this.sessions.filter((x) => x.id !== id);
        return json(200, { id, hidden: true });
      }
    }
    const j = c.path.match(/^\/v1\/jobs\/(.+)$/);
    if (j && c.method === 'GET') {
      const result = this.jobs.get(j[1]);
      if (!result) return err(404, 'job_not_found');
      return json(200, { jobId: j[1], kind: 'open', status: 'done', result });
    }
    return err(404, 'not_found');
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function err(status: number, code: string) {
  return json(status, { error: { code, message: code } });
}
