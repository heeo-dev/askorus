// A pretend PC agent for screen tests. It answers the /v1 routes the app
// uses, keeps a little state, and records what it was asked.

import type { AdapterInfo, Folder, Preview, Session } from '@/api/types';

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
  /** AIs beside Claude that /v1/info lists, such as Codex that only asks (Phase 2). */
  askAgents: AdapterInfo[] = [];
  /** What /v1/info announces; drop 'preview' to act like an older agent. */
  capabilities = ['list', 'preview', 'start', 'resume', 'stop', 'remove', 'open_official', 'serve'];
  /** Previews by session id (F8); 'fail' answers 500, a missing id 404. */
  previews: Record<string, Preview | 'fail'> = {};
  /**
   * Like the real agent, the PUT answer still shows the old server state;
   * the new one appears on the next GET /v1/folders.
   */
  serverLag = true;
  private settle: Array<() => void> = [];
  /** Asks (P2-1/P2-2): what each AI answers; an object makes that AI fail. */
  askReplies: Record<string, string | { code: string; message?: string }> = {};
  /** At most this many AIs of a run have answered; raise it to let the rest finish. */
  runLimit = Infinity;
  /** Opening a session fails with this, as its job's error (for example workspace_not_trusted). */
  openError: { code: string; message: string } | null = null;
  /** POST /v1/runs answers this error instead (for example too_many_runs). */
  runError: { status: number; code: string } | null = null;
  private runs = new Map<string, { id: string; tier: string; prompt: string; parts: any[]; key?: string }>();
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

  /** enableAsk makes this a PC whose Claude, Codex and OpenCode answer questions. */
  enableAsk() {
    if (!this.capabilities.includes('ask')) this.capabilities = [...this.capabilities, 'ask'];
    const tiers = (a: string) => ({
      quick: { model: `${a}-quick`, effort: 'medium' },
      standard: { model: `${a}-std`, effort: 'medium' },
      deep: { model: `${a}-deep`, effort: 'medium' },
    });
    this.askAgents = [
      { agent: 'codex', version: '0.164.0', available: true, capabilities: ['ask'], ask: { tiers: tiers('codex') } },
      { agent: 'opencode', version: '2.2.1', available: true, capabilities: ['ask'], ask: { tiers: tiers('opencode') } },
    ];
  }

  /** The bodies of every POST /v1/runs, in order. */
  runRequests(): any[] {
    return this.callsTo('POST', '/v1/runs').map((c) => c.body);
  }

  private claudeInfo() {
    const ask = this.capabilities.includes('ask')
      ? { ask: { tiers: { quick: { model: 'haiku' }, standard: { model: 'sonnet' }, deep: { model: 'opus' } } } }
      : {};
    return { agent: 'claude', version: '2.1.310', available: true, capabilities: this.capabilities, notices: this.notices, ...ask };
  }

  private runState(r: { id: string; tier: string; parts: any[] }) {
    // Each look lets the next AI finish, up to runLimit.
    const done = r.parts.filter((p) => p.status !== 'running').length;
    const next = r.parts.find((p) => p.status === 'running');
    if (next && done < this.runLimit) {
      const reply = this.askReplies[next.agent];
      Object.assign(next, typeof reply === 'object' ? { status: 'failed', error: reply } : { status: 'done', answer: reply ?? `${next.agent} says: ${next.prompt}` });
    }
    const left = r.parts.some((p) => p.status === 'running');
    const parts = r.parts.map(({ prompt: _p, ...p }) => p);
    return {
      runId: r.id,
      mode: 'ask',
      tier: r.tier,
      status: left ? 'running' : parts.some((p) => p.status === 'done') ? 'done' : 'failed',
      startedAt: '2026-10-30T00:00:00Z',
      parts,
      ...(left ? { pollAfterMs: 1 } : {}),
    };
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
        agents: [this.claudeInfo(), ...this.askAgents],
        capabilities: this.capabilities,
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

    const pv = c.path.match(/^\/v1\/sessions\/([^/]+)\/preview$/);
    if (pv && c.method === 'GET') {
      const p = this.previews[decodeURIComponent(pv[1])];
      if (p === 'fail') return err(500, 'internal');
      return p ? json(200, p) : err(404, 'session_not_found');
    }
    const m = c.path.match(/^\/v1\/sessions\/([^/]+)(?:\/(open|stop))?$/);
    if (m) {
      const id = decodeURIComponent(m[1]);
      const s = this.sessions.find((x) => x.id === id);
      if (!s) return err(404, 'session_not_found');
      if (c.method === 'POST' && m[2] === 'open') {
        const jobId = `job-${++this.n}`;
        if (this.openError) {
          this.jobs.set(jobId, { failed: this.openError });
          return json(202, { jobId, kind: 'open', status: 'running', pollAfterMs: 1 });
        }
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
    if (key === 'POST /v1/runs') {
      if (this.runError) return err(this.runError.status, this.runError.code);
      const known = [...this.runs.values()].find((r) => c.body?.clientRunId && r.key === c.body.clientRunId);
      if (known) return json(200, this.runState(known));
      const id = `run-${++this.n}`;
      const parts = (c.body.agents ?? []).map((t: any) => {
        const sessionId = t.session ?? `${t.agent}:s-${this.n}`;
        // Claude's asks are Claude sessions too; they show under Ask chats.
        if (t.agent === 'claude' && !this.sessions.some((x) => x.id === sessionId)) {
          this.sessions = [session({ id: sessionId, title: c.body.prompt, state: 'history', badges: ['ask'] }), ...this.sessions];
        }
        return { agent: t.agent, status: 'running', session: sessionId, model: t.model ?? `${t.agent}-${c.body.tier}`, ms: 1234, prompt: c.body.prompt };
      });
      const r = { id, tier: c.body.tier ?? 'standard', prompt: c.body.prompt, parts, key: c.body.clientRunId };
      this.runs.set(id, r);
      // Like the agent, the first answer has every AI still running.
      return json(202, {
        runId: id,
        mode: 'ask',
        tier: r.tier,
        status: 'running',
        startedAt: '2026-10-30T00:00:00Z',
        parts: parts.map(({ prompt: _p, ...p }: any) => p),
        pollAfterMs: 1,
      });
    }
    const rn = c.path.match(/^\/v1\/runs\/(.+)$/);
    if (rn && c.method === 'GET') {
      const r = this.runs.get(rn[1]);
      return r ? json(200, this.runState(r)) : err(404, 'run_not_found');
    }
    const j = c.path.match(/^\/v1\/jobs\/(.+)$/);
    if (j && c.method === 'GET') {
      const result = this.jobs.get(j[1]);
      if (!result) return err(404, 'job_not_found');
      if (result.failed) return json(200, { jobId: j[1], kind: 'open', status: 'failed', error: result.failed });
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
