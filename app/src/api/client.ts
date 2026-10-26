// Client for one PC agent's /v1 API. It returns data and stable error
// codes only; the screens pick the words (N9).

import type {
  FolderList,
  Info,
  Job,
  JobStarted,
  OpenResult,
  PairStarted,
  PairStatus,
  Paired,
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

/** The request never got an answer: phone offline, Tailscale off, PC asleep, agent down. */
export class NetworkError extends Error {
  constructor(readonly cause?: unknown) {
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

export function baseUrl(host: string, port: number): string {
  return port === 443 ? `https://${host}` : `https://${host}:${port}`;
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
    this.fetchFn = opts.fetch ?? ((input, init) => fetch(input, init));
    this.sleep = opts.sleep ?? defaultSleep;
    this.timeoutMs = opts.timeoutMs ?? 15000;
  }

  /** A client for the same agent with a device token. */
  withToken(token: string): AgentClient {
    return new AgentClient(this.base, { token, fetch: this.fetchFn, sleep: this.sleep, timeoutMs: this.timeoutMs });
  }

  async request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (body !== undefined) headers['Content-Type'] = 'application/json';

    const ctl = new AbortController();
    const onAbort = () => ctl.abort();
    signal?.addEventListener('abort', onAbort, { once: true });
    const timer = setTimeout(() => ctl.abort(), this.timeoutMs);
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
      throw new NetworkError(e);
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

  stopSession(id: string) {
    return this.request<{ session: Session }>('POST', `sessions/${encodeURIComponent(id)}/stop`);
  }

  removeSession(id: string) {
    return this.request<{ id: string; hidden: boolean }>('DELETE', `sessions/${encodeURIComponent(id)}`);
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

/** Codes may be typed with dashes or spaces. */
export function normalizeCode(code: string): string {
  return code.replace(/[-\s]/g, '').toUpperCase();
}
