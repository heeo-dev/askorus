import { AgentClient, AgentError, CancelledError, NetworkError, baseUrl, comparisonPolling, normalizeCode } from './client';

type Call = { url: string; method: string; headers: Record<string, string>; body?: unknown };

// fakeFetch answers each request from the queue in order and records it.
function fakeFetch(answers: Array<(c: Call) => Response | Error>) {
  const calls: Call[] = [];
  const fn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const c: Call = {
      url: String(input),
      method: init?.method ?? 'GET',
      headers: (init?.headers ?? {}) as Record<string, string>,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    };
    calls.push(c);
    const next = answers.shift();
    if (!next) throw new Error(`unexpected request ${c.method} ${c.url}`);
    const r = next(c);
    if (r instanceof Error) throw r;
    return r;
  }) as typeof fetch;
  return { fn, calls };
}

const json = (status: number, body: unknown) => () =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const dropped = () => () => new TypeError('Network request failed');
const noSleep = async () => {};

const base = 'https://pc.example.ts.net:8790';

test('baseUrl leaves out the default port', () => {
  expect(baseUrl('pc.example.ts.net', 443)).toBe('https://pc.example.ts.net');
  expect(baseUrl('pc.example.ts.net', 8790)).toBe(base);
});

test('requests carry the token and JSON body, and ignore unknown fields', async () => {
  const f = fakeFetch([json(200, { name: 'asksemble-agent', version: '0.1.0', apiVersion: 1, newField: 7 })]);
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn });
  const info = await c.info();
  expect(info.version).toBe('0.1.0');
  expect(f.calls[0].url).toBe(`${base}/v1/info`);
  expect(f.calls[0].headers.Authorization).toBe('Bearer tok');
  expect(f.calls[0].headers['Content-Type']).toBeUndefined();
});

test('no Origin header is sent (the agent refuses cross-origin requests)', async () => {
  const f = fakeFetch([json(200, { sessions: [], warnings: [] })]);
  await new AgentClient(base, { token: 'tok', fetch: f.fn }).sessions();
  expect(Object.keys(f.calls[0].headers).map((h) => h.toLowerCase())).not.toContain('origin');
});

test('session filters become query parameters', async () => {
  const f = fakeFetch([json(200, { sessions: [], warnings: [] })]);
  await new AgentClient(base, { fetch: f.fn }).sessions({ q: '한글 검색', state: 'running,stopped' });
  const u = new URL(f.calls[0].url);
  expect(u.pathname).toBe('/v1/sessions');
  expect(u.searchParams.get('q')).toBe('한글 검색');
  expect(u.searchParams.get('state')).toBe('running,stopped');
});

test('agent errors keep their code', async () => {
  const f = fakeFetch([json(401, { error: { code: 'unauthorized', message: 'device token required' } })]);
  const err = await new AgentClient(base, { fetch: f.fn }).info().catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('unauthorized');
  expect(err.status).toBe(401);
});

test('a non-JSON error page gets an http_ code', async () => {
  const f = fakeFetch([() => new Response('<html>bad gateway</html>', { status: 502 })]);
  const err = await new AgentClient(base, { fetch: f.fn }).info().catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('http_502');
});

test('a dropped request is a NetworkError', async () => {
  const f = fakeFetch([dropped()]);
  await expect(new AgentClient(base, { fetch: f.fn }).info()).rejects.toBeInstanceOf(NetworkError);
});

test('opening a session polls the job until done, through dropped polls', async () => {
  const f = fakeFetch([
    json(202, { jobId: 'j1', kind: 'open', status: 'running', pollAfterMs: 1000 }),
    json(200, { jobId: 'j1', kind: 'open', status: 'running', pollAfterMs: 500 }),
    dropped(),
    json(200, {
      jobId: 'j1',
      kind: 'open',
      status: 'done',
      result: { action: 'resumed', remoteUrl: 'https://claude.ai/code/session_x', session: { id: 'claude:1' } },
    }),
  ]);
  const waits: number[] = [];
  const c = new AgentClient(base, { fetch: f.fn, sleep: async (ms) => void waits.push(ms) });
  let pending = 0;
  const r = await c.openSession('claude:1', { onPending: () => pending++ });
  expect(r.action).toBe('resumed');
  expect(r.remoteUrl).toBe('https://claude.ai/code/session_x');
  expect(f.calls[0].method).toBe('POST');
  expect(f.calls[0].url).toBe(`${base}/v1/sessions/claude%3A1/open`);
  expect(f.calls.slice(1).every((c) => c.url === `${base}/v1/jobs/j1`)).toBe(true);
  expect(waits).toEqual([1000, 500, 500]);
  expect(pending).toBe(1);
});

test('a failed job throws its code', async () => {
  const f = fakeFetch([
    json(202, { jobId: 'j2', kind: 'start', status: 'running', pollAfterMs: 1000 }),
    json(200, { jobId: 'j2', kind: 'start', status: 'failed', error: { code: 'workspace_not_trusted', message: 'x' } }),
  ]);
  const c = new AgentClient(base, { fetch: f.fn, sleep: noSleep });
  const err = await c.startSession({ folder: 'rc-test', name: 'test' }).catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('workspace_not_trusted');
  expect(f.calls[0].body).toEqual({ folder: 'rc-test', name: 'test' });
});

test('waiting stops when cancelled', async () => {
  const f = fakeFetch([json(202, { jobId: 'j3', kind: 'open', status: 'running', pollAfterMs: 1000 })]);
  const ctl = new AbortController();
  const c = new AgentClient(base, {
    fetch: f.fn,
    sleep: async (_ms, signal) => {
      ctl.abort();
      if (signal?.aborted) throw new Error('cancelled');
    },
  });
  await expect(c.openSession('claude:1', { signal: ctl.signal })).rejects.toThrow('cancelled');
});

test('pairing sends the cleaned code and waits for the PC', async () => {
  const f = fakeFetch([
    json(202, { requestId: 'r1', status: 'pending', pollAfterMs: 1000 }),
    json(200, { status: 'pending', pollAfterMs: 1000, apiVersion: 1 }),
    json(200, { status: 'approved', token: 'secret', deviceId: 'd1', deviceName: 'S23', apiVersion: 1 }),
  ]);
  const c = new AgentClient(base, { fetch: f.fn, sleep: noSleep });
  const p = await c.pair('abcd-efgh ijkl-mnop', 'S23');
  expect(p).toEqual({ token: 'secret', deviceId: 'd1', deviceName: 'S23', apiVersion: 1 });
  expect(f.calls[0].body).toEqual({ code: 'ABCDEFGHIJKLMNOP', deviceName: 'S23' });
  expect(f.calls[1].body).toEqual({ requestId: 'r1' });
  expect(f.calls[1].url).toBe(`${base}/v1/pair/status`);
});

test('a pairing refusal surfaces its code', async () => {
  const f = fakeFetch([
    json(202, { requestId: 'r2', status: 'pending', pollAfterMs: 1000 }),
    json(403, { error: { code: 'pair_denied', message: 'denied' } }),
  ]);
  const err = await new AgentClient(base, { fetch: f.fn, sleep: noSleep }).pair('X', 'S23').catch((e) => e);
  expect(err.code).toBe('pair_denied');
});

test('normalizeCode drops dashes and spaces', () => {
  expect(normalizeCode(' ab-cd ef ')).toBe('ABCDEF');
});

// R-17: stop and remove can take longer than the usual timeout (stopping,
// then rm retried while the Claude service settles); they wait longer.
test('stop and remove wait longer than ordinary requests', async () => {
  const slow = ((_input: RequestInfo | URL, init?: RequestInit) =>
    new Promise<Response>((resolve, reject) => {
      const t = setTimeout(() => resolve(new Response(JSON.stringify({ id: 'claude:x', hidden: true }), { status: 200 })), 60);
      init?.signal?.addEventListener('abort', () => {
        clearTimeout(t);
        reject(new TypeError('aborted'));
      });
    })) as typeof fetch;
  const c = new AgentClient(base, { token: 'tok', fetch: slow, timeoutMs: 20 });
  await expect(c.info()).rejects.toBeInstanceOf(NetworkError);
  await expect(c.removeSession('claude:x')).resolves.toEqual({ id: 'claude:x', hidden: true });
  await expect(c.stopSession('claude:x')).resolves.toBeTruthy();
});

// P2-2: asks.
const run = (status: string, parts: unknown[]) => ({ runId: 'r1', mode: 'ask', status, startedAt: 'x', parts, pollAfterMs: 1 });

test('a dropped ask is sent again with the same clientRunId, then followed to the end', async () => {
  const f = fakeFetch([
    dropped(),
    json(202, run('running', [{ agent: 'claude', status: 'running' }])),
    dropped(),
    json(200, run('running', [{ agent: 'claude', status: 'running' }])),
    json(200, { ...run('done', [{ agent: 'claude', status: 'done', answer: '서울' }]), pollAfterMs: undefined }),
  ]);
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: noSleep });
  const req = { mode: 'ask' as const, prompt: 'q', clientRunId: 'k1' };
  const started = await c.startRun(req);
  const seen: string[] = [];
  const done = await c.waitRun(started, { onUpdate: (r) => seen.push(r.status) });
  expect(done.parts[0].answer).toBe('서울');
  expect(seen).toEqual(['running', 'done']);
  expect(f.calls.map((x) => `${x.method} ${x.url.replace(base, '')}`)).toEqual([
    'POST /v1/runs',
    'POST /v1/runs',
    'GET /v1/runs/r1',
    'GET /v1/runs/r1',
    'GET /v1/runs/r1',
  ]);
  expect((f.calls[1].body as { clientRunId: string }).clientRunId).toBe('k1');
});

test('an ask without a clientRunId is not sent twice, and agent errors keep their code', async () => {
  const f = fakeFetch([dropped()]);
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: noSleep });
  await expect(c.startRun({ mode: 'ask', prompt: 'q' })).rejects.toBeInstanceOf(NetworkError);
  expect(f.calls).toHaveLength(1);
  const g = fakeFetch([json(429, { error: { code: 'too_many_runs', message: 'busy' } })]);
  const d = new AgentClient(base, { token: 'tok', fetch: g.fn, sleep: noSleep });
  await expect(d.startRun({ mode: 'ask', prompt: 'q', clientRunId: 'k' })).rejects.toMatchObject({ code: 'too_many_runs' });
  expect(g.calls).toHaveLength(1);
});

// P2-3: answer comparisons.
const cmp = (status: string, over: Record<string, unknown> = {}) => ({
  comparisonId: 'c1',
  status,
  source: 'run',
  engine: 'pc',
  answers: [],
  requests: [],
  judgments: [],
  items: [],
  counts: { differ: 0, check: 0, agree: 0, onlyOne: 0, notCompared: 0, notJudged: 0 },
  ...(status === 'waiting' || status === 'running' ? { pollAfterMs: 1000 } : {}),
  ...over,
});
const paths = (calls: Call[]) => calls.map((x) => `${x.method} ${x.url.replace(base, '')}`);

test('an ask carries its compare, and the run comparison comes back', async () => {
  const f = fakeFetch([
    json(202, { ...run('running', []), comparisonId: 'c1', compareError: { code: 'too_many_comparisons', message: 'busy' } }),
  ]);
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: noSleep });
  const r = await c.startRun({ mode: 'ask', prompt: 'q', clientRunId: 'k', compare: { earlier: ['first'], judges: ['claude', 'codex'] } });
  expect((f.calls[0].body as { compare: unknown }).compare).toEqual({ earlier: ['first'], judges: ['claude', 'codex'] });
  expect(r.comparisonId).toBe('c1');
  expect(r.compareError).toEqual({ code: 'too_many_comparisons', message: 'busy' });
});

test('a comparison is asked for at once, then as often as the PC says, until it is final', async () => {
  const f = fakeFetch([
    json(200, cmp('waiting', { pollAfterMs: 250 })),
    json(200, cmp('running', { pollAfterMs: 400 })),
    json(200, cmp('done')),
  ]);
  const waits: number[] = [];
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: async (ms) => void waits.push(ms) });
  const seen: string[] = [];
  const s = await c.waitComparison('c1', { onUpdate: (x) => seen.push(x.status) });
  expect(s.status).toBe('done');
  expect(seen).toEqual(['waiting', 'running', 'done']);
  expect(waits).toEqual([250, 400]);
  expect(paths(f.calls)).toEqual(['GET /v1/comparisons/c1', 'GET /v1/comparisons/c1', 'GET /v1/comparisons/c1']);
});

test('a final comparison is asked for once; an unknown status polls only while the PC asks to', async () => {
  const f = fakeFetch([json(200, cmp('failed', { error: { code: 'no_judge' } }))]);
  const c = new AgentClient(base, { fetch: f.fn, sleep: noSleep });
  expect((await c.waitComparison('c1')).error?.code).toBe('no_judge');
  expect(f.calls).toHaveLength(1);

  const g = fakeFetch([json(200, cmp('paused', { pollAfterMs: 5 })), json(200, cmp('archived'))]);
  const waits: number[] = [];
  const d = new AgentClient(base, { fetch: g.fn, sleep: async (ms) => void waits.push(ms) });
  expect((await d.waitComparison('c1')).status).toBe('archived');
  expect(waits).toEqual([5]);
  // Running with no pollAfterMs still polls, once a second.
  const h = fakeFetch([json(200, cmp('running', { pollAfterMs: undefined })), json(200, cmp('done'))]);
  const waits2: number[] = [];
  await new AgentClient(base, { fetch: h.fn, sleep: async (ms) => void waits2.push(ms) }).waitComparison('c1');
  expect(waits2).toEqual([1000]);
});

test('waiting for a comparison survives 30 dropped polls in a row, not 31', async () => {
  const ok = fakeFetch([...Array.from({ length: 30 }, dropped), json(200, cmp('done'))]);
  const waits: number[] = [];
  const c = new AgentClient(base, { fetch: ok.fn, sleep: async (ms) => void waits.push(ms) });
  expect((await c.waitComparison('c1')).status).toBe('done');
  expect(ok.calls).toHaveLength(31);
  expect(waits).toEqual(Array.from({ length: 30 }, () => 1000));

  // Misses count again from zero after an answer.
  const reset = fakeFetch([
    ...Array.from({ length: 30 }, dropped),
    json(200, cmp('running')),
    ...Array.from({ length: 30 }, dropped),
    json(200, cmp('done')),
  ]);
  expect((await new AgentClient(base, { fetch: reset.fn, sleep: noSleep }).waitComparison('c1')).status).toBe('done');

  const lost = fakeFetch(Array.from({ length: 31 }, dropped));
  await expect(new AgentClient(base, { fetch: lost.fn, sleep: noSleep }).waitComparison('c1')).rejects.toBeInstanceOf(NetworkError);
  expect(lost.calls).toHaveLength(31);
});

test('a comparison the PC no longer has keeps its code, and cancelling stops waiting', async () => {
  const f = fakeFetch([json(404, { error: { code: 'comparison_not_found', message: 'gone' } })]);
  const err = await new AgentClient(base, { fetch: f.fn, sleep: noSleep }).waitComparison('c1').catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('comparison_not_found');
  expect(err.status).toBe(404);

  const g = fakeFetch([json(200, cmp('running'))]);
  const ctl = new AbortController();
  const c = new AgentClient(base, {
    fetch: g.fn,
    sleep: async (_ms, signal) => {
      ctl.abort();
      if (signal?.aborted) throw new CancelledError();
    },
  });
  await expect(c.waitComparison('c1', { signal: ctl.signal })).rejects.toBeInstanceOf(CancelledError);
  expect(g.calls).toHaveLength(1);
});

test('a dropped comparison request is sent again with the same clientComparisonId, at most three times', async () => {
  const body = { clientComparisonId: 'k1', question: 'q', answers: [{ id: 'claude', agent: 'claude', via: 'pc' as const, text: 'a' }] };
  const f = fakeFetch([dropped(), dropped(), json(202, cmp('waiting', { source: 'direct' }))]);
  const waits: number[] = [];
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: async (ms) => void waits.push(ms) });
  expect((await c.startComparison(body)).source).toBe('direct');
  expect(paths(f.calls)).toEqual(['POST /v1/comparisons', 'POST /v1/comparisons', 'POST /v1/comparisons']);
  expect(f.calls.map((x) => x.body)).toEqual([body, body, body]);
  expect(waits).toEqual([1000, 1000]);

  const g = fakeFetch([dropped(), dropped(), dropped()]);
  await expect(new AgentClient(base, { fetch: g.fn, sleep: noSleep }).startComparison(body)).rejects.toBeInstanceOf(NetworkError);
  expect(g.calls).toHaveLength(3);

  const h = fakeFetch([dropped()]);
  const noKey = { question: body.question, answers: body.answers };
  await expect(new AgentClient(base, { fetch: h.fn, sleep: noSleep }).startComparison(noKey)).rejects.toBeInstanceOf(NetworkError);
  expect(h.calls).toHaveLength(1);

  const i = fakeFetch([json(429, { error: { code: 'too_many_comparisons', message: 'busy' } })]);
  await expect(new AgentClient(base, { fetch: i.fn, sleep: noSleep }).startComparison(body)).rejects.toMatchObject({
    code: 'too_many_comparisons',
  });
  expect(i.calls).toHaveLength(1);
});

test('comparison, retry and cancel use their method and an encoded id', async () => {
  const f = fakeFetch([
    json(200, cmp('done')),
    json(202, cmp('running')),
    json(409, { error: { code: 'comparison_running', message: 'still running' } }),
    json(200, cmp('failed', { error: { code: 'cancelled' } })),
  ]);
  const c = new AgentClient(base, { token: 'tok', fetch: f.fn, sleep: noSleep });
  await c.comparison('c 1/x');
  expect((await c.retryComparison('c 1/x')).status).toBe('running');
  const err = await c.retryComparison('c 1/x').catch((e) => e);
  expect(err).toBeInstanceOf(AgentError);
  expect(err.code).toBe('comparison_running');
  expect(err.status).toBe(409);
  expect((await c.cancelComparison('c 1/x')).error?.code).toBe('cancelled');
  expect(paths(f.calls)).toEqual([
    'GET /v1/comparisons/c%201%2Fx',
    'POST /v1/comparisons/c%201%2Fx/retry',
    'POST /v1/comparisons/c%201%2Fx/retry',
    'POST /v1/comparisons/c%201%2Fx/cancel',
  ]);
  expect(f.calls[1].body).toBeUndefined();
});

test('comparisonPolling: waiting or running, or whatever the PC still wants polled', () => {
  expect(comparisonPolling({ status: 'waiting' })).toBe(true);
  expect(comparisonPolling({ status: 'running' })).toBe(true);
  expect(comparisonPolling({ status: 'done' })).toBe(false);
  expect(comparisonPolling({ status: 'failed' })).toBe(false);
  expect(comparisonPolling({ status: 'later', pollAfterMs: 1 })).toBe(true);
  expect(comparisonPolling({ status: 'later', pollAfterMs: 0 })).toBe(false);
  expect(comparisonPolling({ status: 'later' })).toBe(false);
});
