// A pretend PC agent for screen tests. It answers the /v1 routes the app
// uses, keeps a little state, and records what it was asked.
//
// P2-3: with enableJudge() its AIs also judge, and a run that carries
// compare gets a comparison, judged by the simple rule of fakeCompare.ts.
// Each GET /v1/comparisons/{id} lets the next three judging requests end.

import type {
  AdapterInfo,
  CodeError,
  CompareAnswer,
  Comparison,
  Folder,
  JudgeInfo,
  JudgeRequest,
  JudgingInfo,
  Judgment,
  Preview,
  Session,
} from '@/api/types';

import { aggregate, judgePair, splitUnits } from './fakeCompare';

export const HOST = 'desktop-test.tail0.ts.net';
export const PORT = 8790;
export const CODE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
export const TOKEN = 'device-token';
export const PAIR_LINK = `asksemble://pair?v=1&host=${HOST}&port=${PORT}&code=${CODE}`;

export type Call = { method: string; path: string; query: URLSearchParams; body?: any; auth?: string };

/** The judge models of the agent's defaults. */
export const judgeInfos: Record<string, JudgeInfo> = {
  claude: { model: 'haiku', effort: 'medium', family: 'anthropic' },
  codex: { model: 'gpt-6-luna', effort: 'medium', family: 'openai' },
  opencode: { model: 'deepseek-v4.1-flash', effort: 'high', family: 'deepseek' },
};

/** What a judging AI does: judge, judge but say it is not sure, or fail with this error. */
export type JudgeReply = 'ok' | 'unsure' | { code: string; message?: string };

type FakeRun = { id: string; tier: string; prompt: string; parts: any[]; key?: string; comparisonId?: string };
type Source = { id: string; agent: string; via: string; model?: string; text?: string };

type FakeComparison = {
  id: string;
  source: 'run' | 'direct';
  runId?: string;
  key?: string; // clientComparisonId
  judges: string[];
  question: string;
  earlier: string[];
  status: string;
  answers: CompareAnswer[];
  requests: JudgeRequest[];
  judgments: Judgment[];
  rounds: number; // GETs that let requests end
  error?: CodeError;
};

const batch = 3; // requests that end per GET
const utf8 = (s: string) => new TextEncoder().encode(s).length;
const ended = (r: JudgeRequest) => r.status === 'done' || r.status === 'failed';

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
  private runs = new Map<string, FakeRun>();
  /** Comparisons (P2-3): what each judging AI does (default 'ok'). */
  judgeReplies: Record<string, JudgeReply> = {};
  /** Stop after the first three requests ended; set false to let the rest finish. */
  compareHold = false;
  /** Units past this many in an answer are not judged (notJudged). */
  maxUnits = 40;
  /** Whether judges call every unit core; false leaves only conflicts shown. */
  core = true;
  /** POST /v1/comparisons answers this error instead (for example 429 too_many_comparisons). */
  comparisonError: { status: number; code: string } | null = null;
  /** The route runs, then its reply is lost this many times (the phone gets no answer). */
  loseReplies: Partial<Record<'POST /v1/runs' | 'POST /v1/comparisons', number>> = {};
  /** compareError codes the POST /v1/runs 202s carried, in order. */
  compareErrors: string[] = [];
  private judging: JudgingInfo | null = null;
  private comparisons = new Map<string, FakeComparison>();
  private made: FakeComparison[] = [];
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
    const res = this.route(call);
    const lost = `${call.method} ${call.path}` as keyof FakeAgent['loseReplies'];
    const left = this.loseReplies[lost] ?? 0;
    if (left > 0) {
      this.loseReplies[lost] = left - 1;
      throw new TypeError('Network request failed');
    }
    return res;
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

  /**
   * enableJudge makes the PC's AIs judge (P2-3); call it after enableAsk.
   * verified and typicalMs stand for a judging that has been measured.
   */
  enableJudge(o: { verified?: boolean; typicalMs?: number } = {}) {
    if (!this.capabilities.includes('judge')) this.capabilities = [...this.capabilities, 'judge'];
    this.askAgents = this.askAgents.map((a) =>
      judgeInfos[a.agent] && a.available
        ? { ...a, capabilities: [...new Set([...a.capabilities, 'judge'])], judge: judgeInfos[a.agent] }
        : a,
    );
    this.judging = { engine: 'pc', shape: 'directed', labels: 'v1', rules: 'judge-v1', checks: ['unsure'] };
    if (o.verified) this.judging.verified = { date: '2026-11-08' };
    if (o.typicalMs) this.judging.typicalMs = o.typicalMs;
  }

  /** Like an agent restart: every comparison is gone (404 comparison_not_found). */
  forgetComparisons() {
    this.comparisons.clear();
  }

  /** How many comparisons were made (of one source), also those forgotten since. */
  comparisonCount(source?: 'run' | 'direct'): number {
    return this.made.filter((c) => !source || c.source === source).length;
  }

  /** The ids of the comparisons made, in order. */
  comparisonIds(): string[] {
    return this.made.map((c) => c.id);
  }

  /** Calls to /v1/comparisons… with this method whose path ends with suffix (such as '/retry'). */
  compareCalls(method: string, suffix = ''): Call[] {
    return this.calls.filter((c) => c.method === method && c.path.startsWith('/v1/comparisons') && c.path.endsWith(suffix));
  }

  /** The bodies of every POST /v1/runs, in order. */
  runRequests(): any[] {
    return this.callsTo('POST', '/v1/runs').map((c) => c.body);
  }

  private claudeInfo() {
    const ask = this.capabilities.includes('ask')
      ? { ask: { tiers: { quick: { model: 'haiku' }, standard: { model: 'sonnet' }, deep: { model: 'opus' } } } }
      : {};
    const judge = this.capabilities.includes('judge') ? { judge: judgeInfos.claude } : {};
    return { agent: 'claude', version: '2.1.310', available: true, capabilities: this.capabilities, notices: this.notices, ...ask, ...judge };
  }

  private canJudge(agent: string): boolean {
    if (agent === 'claude') return this.capabilities.includes('judge');
    return this.askAgents.some((a) => a.agent === agent && a.available && a.capabilities.includes('judge'));
  }

  private known(agent: string): boolean {
    return agent === 'claude' || this.askAgents.some((a) => a.agent === agent);
  }

  // badCompare checks POST /v1/runs compare like the agent's parseCompare.
  private badCompare(c: any): boolean {
    if (typeof c !== 'object' || Array.isArray(c)) return true;
    const earlier = c.earlier ?? [];
    const judges = c.judges ?? [];
    if (!Array.isArray(earlier) || !Array.isArray(judges) || earlier.length > 3 || judges.length > 8) return true;
    if (earlier.some((e: unknown) => typeof e !== 'string' || utf8(e) > 4096 || e.includes('\u0000'))) return true;
    return judges.some(
      (j: unknown, i: number) => typeof j !== 'string' || !/^[a-z][a-z0-9-]{0,31}$/.test(j) || judges.indexOf(j) !== i || !this.known(j),
    );
  }

  // runCompare reads POST /v1/runs compare: it makes the run's comparison, or says why not.
  private runCompare(r: FakeRun, compare: any): { compareError?: CodeError } {
    if (compare === undefined || compare === null) return {};
    const code = this.badCompare(compare) ? 'bad_compare' : r.parts.length < 2 ? 'need_two_answers' : '';
    if (code) {
      this.compareErrors.push(code);
      return { compareError: { code, message: code } };
    }
    const asked: string[] = r.parts.map((p) => p.agent);
    const wanted: string[] = (compare.judges ?? []).filter((j: string) => asked.includes(j));
    const cmp = this.newComparison({
      source: 'run',
      runId: r.id,
      judges: (wanted.length ? wanted : asked).filter((j) => this.canJudge(j)),
      question: String(r.prompt ?? '').trim(),
      earlier: compare.earlier ?? [],
    });
    r.comparisonId = cmp.id;
    return {};
  }

  private newComparison(o: Pick<FakeComparison, 'source' | 'runId' | 'key' | 'judges' | 'question' | 'earlier'>): FakeComparison {
    const cmp: FakeComparison = { ...o, id: `cmp-${++this.n}`, status: 'waiting', answers: [], requests: [], judgments: [], rounds: 0 };
    this.comparisons.set(cmp.id, cmp);
    this.made.push(cmp);
    return cmp;
  }

  // plan splits the answers and plans one request per ordered pair, judged
  // by the least busy AI that wrote neither answer and is of neither
  // answer's company; none makes a no_judge record that failed from the start.
  private plan(cmp: FakeComparison, sources: Source[]) {
    cmp.answers = sources.map((src) => {
      const ready = !!src.text?.trim();
      const units = ready ? splitUnits(src.id, src.text ?? '', this.maxUnits) : [];
      const a: CompareAnswer = {
        id: src.id,
        agent: src.agent,
        via: src.via,
        family: judgeInfos[src.agent]?.family ?? '',
        status: ready ? 'ready' : 'missing',
        notJudged: units.filter((u) => !u.judged).length,
        units,
      };
      if (src.model) a.model = src.model;
      return a;
    });
    const ready = cmp.answers.filter((a) => a.status === 'ready');
    if (ready.length < 2) {
      cmp.status = 'failed';
      cmp.error = { code: 'need_two_answers', message: 'a comparison needs answers from at least two AIs' };
      return;
    }
    const pairs: Array<[CompareAnswer, CompareAnswer]> = [];
    for (let i = 0; i < ready.length; i++) for (let j = i + 1; j < ready.length; j++) pairs.push([ready[i], ready[j]], [ready[j], ready[i]]);
    const load = new Map<string, number>();
    cmp.requests = pairs.map(([from, against], k): JudgeRequest => {
      const id = `r${k + 1}`;
      const judge = cmp.judges
        .filter((j) => this.canJudge(j) && j !== from.agent && j !== against.agent)
        .filter((j) => judgeInfos[j]?.family !== from.family && judgeInfos[j]?.family !== against.family)
        .sort((a, b) => (load.get(a) ?? 0) - (load.get(b) ?? 0))[0];
      if (!judge) {
        const error = { code: 'no_judge', message: 'no AI can judge this pair' };
        return { id, from: from.id, against: against.id, judge: '', judgeOnly: false, status: 'failed', attempt: 0, error };
      }
      load.set(judge, (load.get(judge) ?? 0) + 1);
      const judgeOnly = !ready.some((a) => a.agent === judge);
      return { id, from: from.id, against: against.id, judge, judgeOnly, model: judgeInfos[judge]?.model, status: 'queued', attempt: 1 };
    });
    cmp.status = 'running';
    this.startNext(cmp);
    this.settleCompare(cmp);
  }

  // startNext lets the next queued requests run, three at a time.
  private startNext(cmp: FakeComparison) {
    let n = cmp.requests.filter((r) => r.status === 'running').length;
    for (const r of cmp.requests) {
      if (n >= batch) break;
      if (r.status !== 'queued') continue;
      r.status = 'running';
      n++;
    }
  }

  // advance ends the running requests, each as its judge does.
  private advance(cmp: FakeComparison) {
    if (cmp.status !== 'running' || (this.compareHold && cmp.rounds >= 1)) return;
    cmp.rounds++;
    for (const r of cmp.requests) {
      if (r.status !== 'running') continue;
      const reply = this.judgeReplies[r.judge] ?? 'ok';
      cmp.judgments = cmp.judgments.filter((j) => j.request !== r.id);
      if (typeof reply === 'object') {
        Object.assign(r, { status: 'failed', ms: 2100, error: { ...reply } });
        continue;
      }
      Object.assign(r, { status: 'done', ms: 9800, inputTokens: 1200, outputTokens: 300 });
      delete r.error;
      const from = cmp.answers.find((a) => a.id === r.from);
      const against = cmp.answers.find((a) => a.id === r.against);
      const how = { request: r.id, judge: r.judge, unsure: reply === 'unsure', core: this.core };
      if (from && against) cmp.judgments.push(...judgePair(from, against, how));
    }
    this.startNext(cmp);
    this.settleCompare(cmp);
  }

  // settleCompare ends the comparison once every request ended: done when
  // one is done, else failed with the first failure that had a judge.
  private settleCompare(cmp: FakeComparison) {
    if (!cmp.requests.every(ended)) return;
    if (cmp.requests.some((r) => r.status === 'done')) {
      cmp.status = 'done';
      delete cmp.error;
      return;
    }
    const failed = cmp.requests.find((r) => r.judge && r.error) ?? cmp.requests.find((r) => r.error);
    cmp.status = 'failed';
    cmp.error = failed?.error ? { ...failed.error } : { code: 'internal' };
  }

  // look is one GET: a run's comparison waits for the run, is planned on
  // the first look after it ended, and then each look lets requests end.
  private look(cmp: FakeComparison) {
    if (cmp.status !== 'waiting') return this.advance(cmp);
    const r = cmp.runId ? this.runs.get(cmp.runId) : undefined;
    if (r?.parts.some((p) => p.status === 'running')) return;
    this.plan(
      cmp,
      (r?.parts ?? []).map((p) => ({ id: p.agent, agent: p.agent, via: 'pc', model: p.model, text: p.status === 'done' ? p.answer : undefined })),
    );
  }

  private snapshot(cmp: FakeComparison): Comparison {
    const all = cmp.requests.length > 0 && cmp.requests.every(ended);
    const { items, counts } = aggregate(cmp.answers, cmp.requests, cmp.judgments, all);
    const s: Comparison = {
      comparisonId: cmp.id,
      status: cmp.status,
      source: cmp.source,
      engine: 'pc',
      answers: cmp.answers,
      requests: cmp.requests,
      judgments: cmp.judgments,
      items,
      counts,
    };
    if (cmp.runId) s.runId = cmp.runId;
    if (cmp.status === 'waiting' || cmp.status === 'running') s.pollAfterMs = 1;
    if (cmp.error) s.error = cmp.error;
    return s;
  }

  // compareRoute answers /v1/comparisons and its sub-routes, or gives null.
  private compareRoute(c: Call): Response | null {
    if (c.method === 'POST' && c.path === '/v1/comparisons') {
      if (this.comparisonError) return err(this.comparisonError.status, this.comparisonError.code);
      const b = c.body ?? {};
      const again = b.clientComparisonId ? [...this.comparisons.values()].find((x) => x.key === b.clientComparisonId) : undefined;
      if (again) return json(202, this.snapshot(again));
      const answers: any[] = Array.isArray(b.answers) ? b.answers : [];
      if (answers.length < 2) return err(400, 'need_two_answers');
      if (answers.some((a) => typeof a?.text !== 'string' || utf8(a.text) > 65536)) return err(413, 'bad_request');
      const asked: string[] = Array.isArray(b.judges) ? b.judges : [];
      const all = ['claude', ...this.askAgents.map((a) => a.agent)].filter((a) => this.canJudge(a));
      const cmp = this.newComparison({
        source: 'direct',
        key: b.clientComparisonId,
        judges: asked.length ? asked.filter((j) => all.includes(j)) : all,
        question: String(b.question ?? ''),
        earlier: Array.isArray(b.earlier) ? b.earlier : [],
      });
      this.plan(
        cmp,
        answers.map((a) => ({ id: a.id, agent: a.agent, via: a.via, model: a.model, text: a.text })),
      );
      return json(202, this.snapshot(cmp));
    }
    const m = c.path.match(/^\/v1\/comparisons\/([^/]+)(?:\/(retry|cancel))?$/);
    if (!m) return null;
    const cmp = this.comparisons.get(decodeURIComponent(m[1]));
    if (!cmp) return err(404, 'comparison_not_found');
    if (c.method === 'GET' && !m[2]) {
      this.look(cmp);
      return json(200, this.snapshot(cmp));
    }
    if (c.method === 'POST' && m[2] === 'retry') {
      if (cmp.status === 'waiting' || cmp.status === 'running') return err(409, 'comparison_running');
      const again = cmp.requests.filter((r) => r.status === 'failed' && r.judge);
      if (again.length) {
        for (const r of again) {
          Object.assign(r, { status: 'queued', attempt: r.attempt + 1 });
          delete r.error;
          delete r.ms;
        }
        cmp.status = 'running';
        delete cmp.error;
        this.startNext(cmp);
      }
      return json(202, this.snapshot(cmp));
    }
    if (c.method === 'POST' && m[2] === 'cancel') {
      if (cmp.status === 'waiting') {
        cmp.status = 'failed';
        cmp.error = { code: 'cancelled', message: 'cancelled' };
      } else if (cmp.status === 'running') {
        // Queued requests are dropped; running ones would still finish.
        for (const r of cmp.requests) if (r.status === 'queued') Object.assign(r, { status: 'failed', error: { code: 'cancelled' } });
        this.settleCompare(cmp);
      }
      return json(200, this.snapshot(cmp));
    }
    return err(404, 'not_found');
  }

  private runState(r: FakeRun) {
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
      ...(r.comparisonId ? { comparisonId: r.comparisonId } : {}),
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
        ...(this.judging ? { judging: this.judging, integrations: [] } : {}),
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
      const r: FakeRun = { id, tier: c.body.tier ?? 'standard', prompt: c.body.prompt, parts, key: c.body.clientRunId };
      this.runs.set(id, r);
      const compared = this.runCompare(r, c.body.compare);
      // Like the agent, the first answer has every AI still running.
      return json(202, {
        runId: id,
        mode: 'ask',
        tier: r.tier,
        status: 'running',
        startedAt: '2026-10-30T00:00:00Z',
        parts: parts.map(({ prompt: _p, ...p }: any) => p),
        pollAfterMs: 1,
        ...(r.comparisonId ? { comparisonId: r.comparisonId } : {}),
        ...compared,
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
    return this.compareRoute(c) ?? err(404, 'not_found');
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function err(status: number, code: string) {
  return json(status, { error: { code, message: code } });
}
