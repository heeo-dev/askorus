// Client for one PC agent's /v1 API. It returns data and stable error
// codes only; the screens pick the words (N9). Requests go through the
// app's own Tailscale node (tailnet/node, #57), never the phone's ordinary
// network.

import { tailnetFetch } from '@/tailnet/node';

import type {
  Comparison,
  ComparisonRequest,
  Debate,
  DebateRequest,
  FolderList,
  Info,
  Job,
  JobStarted,
  OpenResult,
  PairStarted,
  PairStatus,
  Preview,
  Paired,
  Run,
  RunRequest,
  Session,
  SessionFilter,
  SessionList,
  StartRequest,
} from './types';

/** The agent answered with an error. `code` is stable, `message` is English detail. */
export class AgentError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    message = code,
  ) {
    super(message);
    this.name = 'AgentError';
  }
}

/**
 * The request never got an answer: phone offline, PC asleep, agent down,
 * or the app's Tailscale node could not send it (cause is then a
 * TailnetError with the reason: not signed in, not the person's own PC).
 */
export class NetworkError extends Error {
  constructor(
    readonly cause?: unknown,
    /** No answer before the client's timeout (rather than an error right away). */
    readonly timedOut = false,
  ) {
    super('network');
    this.name = 'NetworkError';
  }
}

/** The person stopped waiting. */
export class CancelledError extends Error {
  constructor() {
    super('cancelled');
    this.name = 'CancelledError';
  }
}

export type ClientOptions = {
  token?: string;
  fetch?: typeof fetch;
  sleep?: (ms: number, signal?: AbortSignal) => Promise<void>;
  timeoutMs?: number;
};

export type WaitOptions = {
  signal?: AbortSignal;
  /** Called after each answer that is not final yet. */
  onPending?: () => void;
};

// A dropped connection while waiting is asked again this many times in a
// row before giving up (about half a minute at one poll a second).
const maxMisses = 30;

// Plain http, inside the tailnet only: Tailscale already encrypts between
// the phone's node and the PC's and knows who is on each end, so the PC
// needs no certificate (and no name in the public certificate logs).
export function baseUrl(host: string, port: number): string {
  return `http://${host}:${port}`;
}

export function defaultSleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(new CancelledError());
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(t);
      reject(new CancelledError());
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

const slowActionMs = 45000;

export class AgentClient {
  private readonly token?: string;
  private readonly fetchFn: typeof fetch;
  private readonly sleep: (ms: number, signal?: AbortSignal) => Promise<void>;
  private readonly timeoutMs: number;

  constructor(
    readonly base: string,
    opts: ClientOptions = {},
  ) {
    this.token = opts.token;
    this.fetchFn = opts.fetch ?? tailnetFetch;
    this.sleep = opts.sleep ?? defaultSleep;
    this.timeoutMs = opts.timeoutMs ?? 15000;
  }

  /** A client for the same agent with a device token. */
  withToken(token: string): AgentClient {
    return new AgentClient(this.base, { token, fetch: this.fetchFn, sleep: this.sleep, timeoutMs: this.timeoutMs });
  }

  async request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal, timeoutMs = this.timeoutMs): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (body !== undefined) headers['Content-Type'] = 'application/json';

    const ctl = new AbortController();
    const onAbort = () => ctl.abort();
    signal?.addEventListener('abort', onAbort, { once: true });
    const timer = setTimeout(() => ctl.abort(), timeoutMs);
    let res: Response;
    try {
      res = await this.fetchFn(`${this.base}/v1/${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: ctl.signal,
      });
    } catch (e) {
      if (signal?.aborted) throw new CancelledError();
      throw new NetworkError(e, ctl.signal.aborted);
    } finally {
      clearTimeout(timer);
      signal?.removeEventListener('abort', onAbort);
    }

    let data: unknown = null;
    try {
      data = await res.json();
    } catch {
      // not JSON: a proxy error page or an empty body
    }
    if (!res.ok) {
      const err = (data as { error?: { code?: string; message?: string } } | null)?.error;
      throw new AgentError(err?.code || `http_${res.status}`, res.status, err?.message);
    }
    return data as T;
  }

  info(signal?: AbortSignal) {
    return this.request<Info>('GET', 'info', undefined, signal);
  }

  sessions(filter: SessionFilter = {}, signal?: AbortSignal) {
    const q = new URLSearchParams();
    if (filter.q) q.set('q', filter.q);
    if (filter.folder) q.set('folder', filter.folder);
    if (filter.state) q.set('state', filter.state);
    const s = q.toString();
    return this.request<SessionList>('GET', s ? `sessions?${s}` : 'sessions', undefined, signal);
  }

  folders(signal?: AbortSignal) {
    return this.request<FolderList>('GET', 'folders', undefined, signal);
  }

  setFolderServer(folderId: string, enabled: boolean, opts: { spawn?: string; capacity?: number } = {}) {
    return this.request<FolderList['folders'][number]>('PUT', `folders/${encodeURIComponent(folderId)}/server`, {
      enabled,
      ...opts,
    });
  }

  /** Opens a session (wakes or resumes it if needed) and waits for the remote link. */
  async openSession(id: string, wait: WaitOptions = {}): Promise<OpenResult> {
    const started = await this.request<JobStarted>('POST', `sessions/${encodeURIComponent(id)}/open`, undefined, wait.signal);
    return this.waitJob(started, wait);
  }

  /** Starts a new background session and waits for its remote link. */
  async startSession(req: StartRequest, wait: WaitOptions = {}): Promise<OpenResult> {
    const started = await this.request<JobStarted>('POST', 'sessions', req, wait.signal);
    return this.waitJob(started, wait);
  }

  // Stop waits until the process is gone, and remove may stop first and
  // then retry rm while the Claude service settles (R-17): together up to
  // about 25 s on this PC, past the usual 15 s.
  stopSession(id: string) {
    return this.request<{ session: Session }>('POST', `sessions/${encodeURIComponent(id)}/stop`, undefined, undefined, slowActionMs);
  }

  removeSession(id: string) {
    return this.request<{ id: string; hidden: boolean }>('DELETE', `sessions/${encodeURIComponent(id)}`, undefined, undefined, slowActionMs);
  }

  /** The last question and answer of a session, read only (F8). Shown, never stored. */
  preview(id: string, signal?: AbortSignal) {
    return this.request<Preview>('GET', `sessions/${encodeURIComponent(id)}/preview`, undefined, signal);
  }

  /**
   * Asks the PC's AIs (POST /v1/runs, mode ask). A dropped request is sent
   * again with the same clientRunId, so the PC does not ask twice.
   */
  async startRun(req: RunRequest, signal?: AbortSignal): Promise<Run> {
    for (let tries = 0; ; tries++) {
      try {
        return await this.request<Run>('POST', 'runs', req, signal);
      } catch (e) {
        if (!(e instanceof NetworkError) || !req.clientRunId || tries >= 2) throw e;
        await this.sleep(1000, signal);
      }
    }
  }

  run(runId: string, signal?: AbortSignal) {
    return this.request<Run>('GET', `runs/${encodeURIComponent(runId)}`, undefined, signal);
  }

  /**
   * Asks for a run until every AI has answered, handing each new state to
   * onUpdate so answers show as they come. Dropped requests are retried
   * like waitJob.
   */
  async waitRun(started: Run, wait: WaitOptions & { onUpdate?: (r: Run) => void } = {}): Promise<Run> {
    let cur = started;
    let misses = 0;
    while (cur.status === 'running') {
      await this.sleep(cur.pollAfterMs || 1000, wait.signal);
      try {
        cur = await this.run(started.runId, wait.signal);
      } catch (e) {
        if (e instanceof NetworkError && misses++ < maxMisses) continue;
        throw e;
      }
      misses = 0;
      wait.onUpdate?.(cur);
    }
    return cur;
  }

  /** One answer comparison (P2-3). 404 comparison_not_found: gone from the PC (a day, restart). */
  comparison(id: string, signal?: AbortSignal) {
    return this.request<Comparison>('GET', `comparisons/${encodeURIComponent(id)}`, undefined, signal);
  }

  /**
   * Asks for a comparison at once and then until it is final, handing each
   * state to onUpdate so the combined answer shows as it is written. It keeps
   * asking while the PC says so (pollAfterMs), also for a status this app
   * does not know yet. Dropped requests are retried like waitRun; agent
   * errors (404) and cancelling end it.
   */
  async waitComparison(id: string, wait: WaitOptions & { onUpdate?: (c: Comparison) => void } = {}): Promise<Comparison> {
    let misses = 0;
    for (;;) {
      let cur: Comparison;
      try {
        cur = await this.comparison(id, wait.signal);
      } catch (e) {
        if (!(e instanceof NetworkError) || misses++ >= maxMisses) throw e;
        await this.sleep(1000, wait.signal);
        continue;
      }
      misses = 0;
      wait.onUpdate?.(cur);
      if (!comparisonPolling(cur)) return cur;
      await this.sleep(cur.pollAfterMs || 1000, wait.signal);
    }
  }

  /**
   * Compares answers the phone sends (POST /v1/comparisons). Like startRun,
   * a dropped request is sent again only with a clientComparisonId, so the
   * PC makes one comparison.
   */
  async startComparison(req: ComparisonRequest, signal?: AbortSignal): Promise<Comparison> {
    for (let tries = 0; ; tries++) {
      try {
        return await this.request<Comparison>('POST', 'comparisons', req, signal);
      } catch (e) {
        if (!(e instanceof NetworkError) || !req.clientComparisonId || tries >= 2) throw e;
        await this.sleep(1000, signal);
      }
    }
  }

  /** Starts a comparison that failed over (202). 409 comparison_running while it still runs. */
  retryComparison(id: string, signal?: AbortSignal) {
    return this.request<Comparison>('POST', `comparisons/${encodeURIComponent(id)}/retry`, undefined, signal);
  }

  /**
   * Writes a comparison's combined answer (S5b), again after it failed or
   * when it was off (202). agent is the AI the person picked; left out, the
   * PC picks. One already written comes back unchanged. 409
   * comparison_running while the comparison or the writing still runs.
   */
  writeSynthesis(id: string, agent?: string, signal?: AbortSignal) {
    return this.request<Comparison>('POST', `comparisons/${encodeURIComponent(id)}/synthesis`, agent ? { agent } : {}, signal);
  }

  /** One debate (P2-5). 404 debate_not_found: gone from the PC (a day, restart). */
  debate(id: string, signal?: AbortSignal) {
    return this.request<Debate>('GET', `debates/${encodeURIComponent(id)}`, undefined, signal);
  }

  /** Starts a debate (202); like startComparison, a dropped request goes again only with a clientDebateId. */
  async startDebate(req: DebateRequest, signal?: AbortSignal): Promise<Debate> {
    for (let tries = 1; ; tries++) {
      try {
        return await this.request<Debate>('POST', 'debates', req, signal);
      } catch (e) {
        if (!(e instanceof NetworkError) || !req.clientDebateId || tries >= 2) throw e;
      }
    }
  }

  /**
   * Asks for a debate until it has ended (no pollAfterMs), handing each
   * look to onUpdate; dropped requests are retried like waitComparison.
   */
  async waitDebate(id: string, wait: WaitOptions & { onUpdate?: (d: Debate) => void } = {}): Promise<Debate> {
    let misses = 0;
    for (;;) {
      let cur: Debate;
      try {
        cur = await this.debate(id, wait.signal);
      } catch (e) {
        if (!(e instanceof NetworkError) || misses++ >= maxMisses) throw e;
        await this.sleep(1000, wait.signal);
        continue;
      }
      misses = 0;
      wait.onUpdate?.(cur);
      if (!cur.pollAfterMs) return cur;
      await this.sleep(cur.pollAfterMs, wait.signal);
    }
  }

  /** Stops a debate: it ends stopped at once; an ended one comes back as it is. */
  stopDebate(id: string) {
    return this.request<Debate>('POST', `debates/${encodeURIComponent(id)}/stop`);
  }

  /** Stops a comparison: a combined answer not started yet is dropped, one being written finishes. */
  cancelComparison(id: string) {
    return this.request<Comparison>('POST', `comparisons/${encodeURIComponent(id)}/cancel`);
  }

  /** Unpairs this phone on the PC, so its token stops working there. */
  forgetSelf() {
    return this.request<{ deviceId: string; removed: boolean }>('DELETE', 'devices/self');
  }

  job(jobId: string, signal?: AbortSignal) {
    return this.request<Job>('GET', `jobs/${encodeURIComponent(jobId)}`, undefined, signal);
  }

  /**
   * Asks for a job's result until it is done. Leaving the app drops open
   * requests, so the agent answers at once and the phone asks again; a
   * dropped connection while asking is retried.
   */
  async waitJob(started: Pick<JobStarted, 'jobId' | 'pollAfterMs'>, wait: WaitOptions = {}): Promise<OpenResult> {
    let every = started.pollAfterMs || 1000;
    let misses = 0;
    for (;;) {
      await this.sleep(every, wait.signal);
      let st: Job;
      try {
        st = await this.job(started.jobId, wait.signal);
      } catch (e) {
        if (e instanceof NetworkError && misses++ < maxMisses) continue;
        throw e;
      }
      misses = 0;
      if (st.status === 'done' && st.result) return st.result;
      if (st.status === 'failed') {
        throw new AgentError(st.error?.code || 'launch_failed', 200, st.error?.message);
      }
      every = st.pollAfterMs || every;
      wait.onPending?.();
    }
  }

  /** Sends the pairing code from the PC's QR and waits until the person at the PC answers. */
  async pair(code: string, deviceName: string, wait: WaitOptions = {}): Promise<Paired> {
    const started = await this.request<PairStarted>(
      'POST',
      'pair',
      { code: normalizeCode(code), deviceName },
      wait.signal,
    );
    return this.waitPair(started, wait);
  }

  async waitPair(started: Pick<PairStarted, 'requestId' | 'pollAfterMs'>, wait: WaitOptions = {}): Promise<Paired> {
    let every = started.pollAfterMs || 1000;
    let misses = 0;
    for (;;) {
      await this.sleep(every, wait.signal);
      let st: PairStatus;
      try {
        st = await this.request<PairStatus>('POST', 'pair/status', { requestId: started.requestId }, wait.signal);
      } catch (e) {
        if (e instanceof NetworkError && misses++ < maxMisses) continue;
        throw e;
      }
      misses = 0;
      if (st.status === 'approved' && st.token && st.deviceId) {
        return { token: st.token, deviceId: st.deviceId, deviceName: st.deviceName ?? deviceNameFallback, apiVersion: st.apiVersion };
      }
      every = st.pollAfterMs || every;
      wait.onPending?.();
    }
  }
}

const deviceNameFallback = 'phone';

/**
 * comparisonPolling: a comparison is asked for again while it waits or
 * runs, and also while the PC still asks to be polled (pollAfterMs): its
 * combined answer is still being written (S5b), or a newer status.
 */
export function comparisonPolling(c: Pick<Comparison, 'status' | 'pollAfterMs'>): boolean {
  return c.status === 'waiting' || c.status === 'running' || (c.pollAfterMs ?? 0) > 0;
}

/** Codes may be typed with dashes or spaces. */
export function normalizeCode(code: string): string {
  return code.replace(/[-\s]/g, '').toUpperCase();
}
