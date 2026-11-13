// A pretend PC agent for screen tests. It answers the /v1 routes the app
// uses, keeps a little state, and records what it was asked.
//
// P2-3 / S5b: with enableSynth() its AIs also write combined answers, and a
// run that carries compare gets a comparison. Like the agent since C6
// (#19, api.Options.NoJudging) a comparison asks no judge and makes no
// card: it is done as soon as the answers are in (on the first GET after
// the run ended), and then the moderator writes. It takes its synthesis
// from the request (none: off) and always reports it: the next GET starts
// the writing (running) and the one after ends it (combinedText).

import { agentName } from '@/api/agents';
import type { AdapterInfo, CodeError, Comparison, Debate, DebatePoint, Folder, Preview, Session, Synthesis } from '@/api/types';

export const HOST = 'desktop-test.tail0.ts.net';
export const PORT = 8790;
export const CODE = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
export const TOKEN = 'device-token';
export const PAIR_LINK = `askorus://pair?v=2&host=${HOST}&port=${PORT}&code=${CODE}`;

export type Call = { method: string; path: string; query: URLSearchParams; body?: any; auth?: string };

/** What the writing AI does (S5b): write, fail (usage limit), or keep writing. */
export type SynthReply = 'ok' | 'fail' | 'hold';

type FakeRun = { id: string; tier: string; prompt: string; parts: any[]; key?: string; comparisonId?: string; refine?: any };
type Source = { id: string; agent: string; via: string; model?: string; text?: string };

// An answer of a comparison: ready when it has text.
type FakeAnswer = { id: string; agent: string; ready: boolean };

type FakeComparison = {
  id: string;
  source: 'run' | 'direct';
  runId?: string;
  key?: string; // clientComparisonId
  question: string;
  earlier: string[];
  status: string;
  answers: FakeAnswer[];
  error?: CodeError;
  asked: string[]; // who may write the combined answer, in turn order
  synth?: FakeSynth; // only on a PC that writes (enableSynth)
};

/** The template the fake moderator sends with a comparison question (milestone C). */
export const REFINE_TEMPLATE =
  'Answer format (added by Askorus, not written by the person asking)\n- Give the conclusion in one line first.\n- Compare in a Markdown table (a row per criterion, a column per thing compared).\n- End with what to look at when choosing.\n- Keep it short (within 3–5 sentences).';

// The combined answer's state; want is the AI asked for, tier the question's.
type FakeSynth = Omit<Synthesis, 'agent'> & { agent?: string; want?: string; tier: string };

const utf8 = (s: string) => new TextEncoder().encode(s).length;

/**
 * combinedText is the pretend moderator's combined answer: a first line,
 * then the AIs whose answers it read, each named in braces as the writer
 * names an AI (#6), so the screen draws them as chips.
 */
function combinedText(names: string[]): string {
  return ['Fake combined answer.', `From ${names.map((n) => `{{${n}}}`).join(', ')}.`].join('\n\n');
}

// A debate: the request it was made from and how far it went (each GET is a step).
type FakeDebate = { id: string; key?: string; body: any; step: number; stopped?: boolean; reply: FakeAgent['debateReply'] };

/** The fake moderator's answer after the debate, naming the AIs that spoke. */
export const DEBATE_AFTER = 'Fake answer after the debate.';

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
  /** The system this PC's info reports (#53). */
  os = 'windows';
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
  /** POST /v1/comparisons answers this error instead (for example 429 too_many_comparisons). */
  comparisonError: { status: number; code: string } | null = null;
  /** The route runs, then its reply is lost this many times (the phone gets no answer). */
  loseReplies: Partial<Record<'POST /v1/runs' | 'POST /v1/comparisons', number>> = {};
  /** compareError codes the POST /v1/runs 202s carried, in order. */
  compareErrors: string[] = [];
  /** Combined answers (S5b): what the writing AI does (default 'ok'). */
  synthReply: SynthReply = 'ok';
  /**
   * Question refining (milestone C): what the moderator does with a run that
   * asks for it: a comparison spec with its template (default), an open
   * question without one, or a failure (the question went as written). The
   * POST's 202 shows it running; the first GET has it ended.
   */
  refineReply: 'done' | 'open' | 'failed' = 'done';
  /** How many combined answers were started (each takes one more AI call). */
  synthCalls = 0;
  private synthOn = false;
  private comparisons = new Map<string, FakeComparison>();
  private made: FakeComparison[] = [];
  /**
   * Debates (P2-5): what the moderator does: a debate on one point (each
   * GET moves it one step: planned, a turn running, done, the next…, the
   * check, the outcome, the answer after it), no point worth one, a failed
   * plan, or one that stops at the first turn running.
   */
  debateReply: 'ok' | 'none' | 'fail' | 'hold' = 'ok';
  /** POST /v1/debates answers this error instead (for example 429 too_many_debates). */
  debateError: { status: number; code: string } | null = null;
  private debateOn = false;
  private contextOn = false;
  private moderatorOn = false;
  private debates = new Map<string, FakeDebate>();
  /** How the PC can be reached: fine, or not at all (off, asleep, or its agent stopped). */
  reach: 'ok' | 'offline' = 'ok';
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

  /** askModels gives Codex or OpenCode these tier models and, as newer agents do, their efforts (11-06). */
  askModels(agent: string, tiers: Record<string, { model: string; effort?: string }>, efforts?: Record<string, string[]>) {
    this.askAgents = this.askAgents.map((a) => (a.agent === agent ? { ...a, ask: { tiers, ...(efforts ? { efforts } : {}) } } : a));
  }

  /**
   * enableSynth makes the PC's AIs write combined answers (S5b; since C6
   * the moderator alone, from the answers); call it after enableAsk.
   */
  enableSynth() {
    this.synthOn = true;
    if (!this.capabilities.includes('synthesize')) this.capabilities = [...this.capabilities, 'synthesize'];
    this.askAgents = this.askAgents.map((a) => (a.available ? { ...a, capabilities: [...new Set([...a.capabilities, 'synthesize'])] } : a));
  }

  /** enableDebate makes the agent run debates (P2-5: /v1/info's debate and /v1/debates); call it after enableSynth. */
  enableDebate() {
    this.debateOn = true;
  }

  /** enableContext makes the agent take a conversation record (#29: /v1/info's context). */
  enableContext() {
    this.contextOn = true;
  }

  /** enableModerator makes the agent ask the moderator alone (#25: /v1/info's moderator); call it after enableContext. */
  enableModerator() {
    this.moderatorOn = true;
  }

  /** The bodies of every POST /v1/debates, in order. */
  debateRequests(): any[] {
    return this.callsTo('POST', '/v1/debates').map((c) => c.body);
  }

  /** releaseDebates lets the debates held at their first turn (debateReply 'hold') go on. */
  releaseDebates() {
    for (const d of this.debates.values()) if (d.reply === 'hold') d.reply = 'ok';
  }

  /** Like an agent restart: every debate is gone (404 debate_not_found). */
  forgetDebates() {
    this.debates.clear();
  }

  // debateSnap is a debate as far as its step: planned, then each of the
  // two turns running and done, the check, the outcome, writing and done.
  private debateSnap(d: FakeDebate): Debate {
    const pcs: string[] = (d.body.answers ?? []).filter((a: any) => a.via === 'pc').map((a: any) => a.agent);
    const [a, b] = [pcs[0] ?? 'claude', pcs[1] ?? 'codex'];
    const base = { debateId: d.id, moderator: d.body.moderator || a, model: 'claude-sonnet', tier: d.body.tier || 'standard', calls: d.step, ms: d.step * 10000 };
    const live = (x: Debate): Debate => (d.stopped ? x : { ...x, pollAfterMs: 1 });
    if (d.step === 0) return live({ ...base, status: 'planning', points: [] });
    if (d.reply === 'none') return { ...base, status: 'done', points: [] };
    if (d.reply === 'fail') return { ...base, status: 'failed', points: [], error: { code: 'judge_bad_output', message: 'bad' } };
    const turn = (ai: string, i: number, running: boolean) => ({
      ai,
      ask: `Question to ${agentName(ai)}?`,
      status: running ? 'running' : 'done',
      ms: 3000 + i * 1000,
      ...(running ? {} : { text: `${agentName(ai)} turn: the timetable says 8 a.m.`, stance: i === 0 ? 'keep' : 'change', model: `${ai}-std` }),
    });
    const p1: DebatePoint = {
      id: 'p1',
      topic: 'First ferry time',
      nature: 'fact',
      debate: true,
      goal: 'the time checked with a source',
      sides: [
        { ais: [a], says: '7 a.m.' },
        { ais: pcs.slice(1).length ? pcs.slice(1) : [b], says: '8 a.m.' },
      ],
      turns: [],
    };
    const p2: DebatePoint = {
      id: 'p2',
      topic: 'Where to buy tickets',
      nature: 'judgment',
      debate: false,
      sides: [
        { ais: [a], says: 'the port' },
        { ais: [b], says: 'the app' },
      ],
      turns: [],
    };
    const step = d.step;
    if (step >= 2) p1.turns.push(turn(a, 0, step === 2));
    if (step >= 4) p1.turns.push(turn(b, 1, step === 4));
    if (step === 6) p1.checking = true;
    if (step >= 7) Object.assign(p1, { result: 'agreed', line: `The first ferry is at 8 a.m. ({{${agentName(a)}}} changed).` });
    const points = [p1, p2];
    if (d.stopped) {
      for (const p of points) {
        delete p.checking;
        for (const x of p.turns) if (x.status === 'running') Object.assign(x, { status: 'failed', error: { code: 'cancelled', message: 'stopped' } });
      }
      return { ...base, status: 'stopped', points };
    }
    if (step <= 7) return live({ ...base, status: 'debating', points });
    if (step === 8) return live({ ...base, status: 'writing', points });
    const after = `${DEBATE_AFTER}\n\nFrom {{${agentName(a)}}} and {{${agentName(b)}}}.`;
    return { ...base, status: 'done', points, after: { text: after, agent: base.moderator, model: 'claude-sonnet', ms: 9000 } };
  }

  // debateRoute answers /v1/debates and its sub-routes, or gives null.
  private debateRoute(c: Call): Response | null {
    if (!c.path.startsWith('/v1/debates')) return null;
    if (!this.debateOn) return err(404, 'not_found');
    if (c.method === 'POST' && c.path === '/v1/debates') {
      if (this.debateError) return err(this.debateError.status, this.debateError.code);
      const b = c.body ?? {};
      const again = b.clientDebateId ? [...this.debates.values()].find((x) => x.key === b.clientDebateId) : undefined;
      if (again) return json(202, this.debateSnap(again));
      if (!Array.isArray(b.answers) || b.answers.length < 2) return err(400, 'need_two_answers');
      if (typeof b.synthesis !== 'string' || !b.synthesis.trim()) return err(400, 'bad_request');
      if ([...this.debates.values()].some((x) => this.debateSnap(x).pollAfterMs)) return err(429, 'too_many_debates');
      const d: FakeDebate = { id: `dbt-${++this.n}`, key: b.clientDebateId, body: b, step: 0, reply: this.debateReply };
      this.debates.set(d.id, d);
      return json(202, this.debateSnap(d));
    }
    const m = c.path.match(/^\/v1\/debates\/([^/]+)(?:\/(stop))?$/);
    if (!m) return err(404, 'not_found');
    const d = this.debates.get(decodeURIComponent(m[1]));
    if (!d) return err(404, 'debate_not_found');
    if (c.method === 'GET' && !m[2]) {
      const hold = d.reply === 'hold' && d.step >= 2;
      if (!hold && this.debateSnap(d).pollAfterMs) d.step++;
      return json(200, this.debateSnap(d));
    }
    if (c.method === 'POST' && m[2] === 'stop') {
      if (this.debateSnap(d).pollAfterMs) d.stopped = true;
      return json(200, this.debateSnap(d));
    }
    return err(404, 'not_found');
  }

  /** The bodies of every POST /v1/comparisons/{id}/synthesis, in order. */
  synthRequests(): any[] {
    return this.compareCalls('POST', '/synthesis').map((c) => c.body);
  }

  /** Like a run kept past a day, or an agent restart: every run is gone (404 run_not_found). */
  forgetRuns() {
    this.runs.clear();
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
    return { agent: 'claude', version: '2.1.310', available: true, capabilities: this.capabilities, notices: this.notices, ...ask };
  }

  private known(agent: string): boolean {
    return agent === 'claude' || this.askAgents.some((a) => a.agent === agent);
  }

  // canWrite: the AI may write combined answers now (the synthesize capability, as the agent announces it).
  private canWrite(agent: string): boolean {
    if (!this.synthOn) return false;
    if (agent === 'claude') return this.capabilities.includes('synthesize');
    return this.askAgents.some((a) => a.agent === agent && a.available && a.capabilities.includes('synthesize'));
  }

  // badSynthesis checks a request's synthesis like the agent: an object
  // whose agent is '' or an AI of this PC, off a boolean, tier (direct
  // comparisons) one of the tiers. null is no synthesis.
  private badSynthesis(y: any, withTier: boolean): boolean {
    if (y === undefined || y === null) return false;
    if (typeof y !== 'object' || Array.isArray(y)) return true;
    if (y.agent !== undefined && (typeof y.agent !== 'string' || (y.agent !== '' && !this.known(y.agent)))) return true;
    if (y.off !== undefined && typeof y.off !== 'boolean') return true;
    if (withTier && y.tier !== undefined && !['', 'quick', 'standard', 'deep'].includes(y.tier)) return true;
    return false;
  }

  // newSynth makes a comparison's synthesis from its request: off unless asked.
  private newSynth(y: any, tier: string): FakeSynth | undefined {
    if (!this.synthOn) return undefined;
    const on = !!y && typeof y === 'object' && !y.off;
    const out: FakeSynth = { status: on ? 'waiting' : 'off', tier: tier || 'standard' };
    if (on && y.agent) out.want = y.agent;
    return out;
  }

  // resetSynth makes a synthesis wait to be written again (by want, or the AI the PC picks).
  private resetSynth(y: FakeSynth, want?: string) {
    const written = ['agent', 'model', 'attempt', 'ms', 'inputTokens', 'outputTokens', 'text', 'error'] as const;
    for (const k of written) delete y[k];
    y.status = 'waiting';
    if (want !== undefined) {
      if (want) y.want = want;
      else delete y.want;
    }
  }

  private tierModel(agent: string, tier: string): string {
    const info = (agent === 'claude' ? this.claudeInfo() : this.askAgents.find((a) => a.agent === agent)) as AdapterInfo | undefined;
    return info?.ask?.tiers?.[tier]?.model ?? `${agent}-${tier}`;
  }

  // synthStep moves a comparison's synthesis on, from what the last look
  // left: once the comparison ended a waiting one starts (one more AI
  // call), and a running one ends as synthReply says.
  private synthStep(cmp: FakeComparison) {
    const y = cmp.synth;
    if (!y) return;
    if (y.status === 'waiting' && (cmp.status === 'done' || cmp.status === 'failed')) {
      if (cmp.answers.filter((a) => a.ready).length < 2) {
        Object.assign(y, { status: 'failed', error: { code: 'need_two_answers', message: 'a combined answer needs two answers' } });
        return;
      }
      // The AI asked for if it was asked and may write, else the first asked AI that may.
      const wanted = y.want && cmp.asked.includes(y.want) && this.canWrite(y.want) ? y.want : undefined;
      const writer = wanted ?? cmp.asked.find((a) => this.canWrite(a));
      if (!writer) {
        Object.assign(y, { status: 'failed', error: { code: 'no_judge', message: 'no AI may write the combined answer' } });
        return;
      }
      Object.assign(y, { status: 'running', agent: writer, model: this.tierModel(writer, y.tier), attempt: 1 });
      this.synthCalls++;
      return;
    }
    if (y.status !== 'running' || this.synthReply === 'hold') return;
    if (this.synthReply === 'fail') {
      Object.assign(y, { status: 'failed', ms: 2100, error: { code: 'judge_failed', message: 'usage limit reached' } });
      return;
    }
    const names = cmp.answers.filter((a) => a.ready).map((a) => agentName(a.agent));
    Object.assign(y, { status: 'done', ms: 15200, inputTokens: 5200, outputTokens: 640, text: combinedText(names) });
  }

  // synthSnap is the snapshot's synthesis: the AI asked for until one is picked, the text only when done.
  private synthSnap(y: FakeSynth): Synthesis {
    const { want, tier: _tier, text, ...rest } = y;
    const out: Synthesis = { ...rest };
    if (!out.agent && want) out.agent = want;
    if (y.status === 'done' && text !== undefined) out.text = text;
    return out;
  }

  // badCompare checks POST /v1/runs compare like the agent's parseCompare.
  private badCompare(c: any): boolean {
    if (typeof c !== 'object' || Array.isArray(c)) return true;
    // An older agent ignores synthesis; one that writes checks it.
    if (this.synthOn && this.badSynthesis(c.synthesis, false)) return true;
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
    const cmp = this.newComparison({
      source: 'run',
      runId: r.id,
      question: String(r.prompt ?? '').trim(),
      earlier: compare.earlier ?? [],
      asked,
      synth: this.newSynth(compare.synthesis, r.tier),
    });
    r.comparisonId = cmp.id;
    return {};
  }

  private newComparison(o: Pick<FakeComparison, 'source' | 'runId' | 'key' | 'question' | 'earlier' | 'asked' | 'synth'>): FakeComparison {
    const cmp: FakeComparison = { ...o, id: `cmp-${++this.n}`, status: 'waiting', answers: [] };
    this.comparisons.set(cmp.id, cmp);
    this.made.push(cmp);
    return cmp;
  }

  // plan takes the answers in, as the agent with NoJudging does: no judge
  // request, so the comparison is done at once, or failed with fewer than
  // two answers.
  private plan(cmp: FakeComparison, sources: Source[]) {
    cmp.answers = sources.map((src) => ({ id: src.id, agent: src.agent, ready: !!src.text?.trim() }));
    this.conclude(cmp);
  }

  // conclude ends a comparison from its answers: done with two or more, else failed.
  private conclude(cmp: FakeComparison) {
    if (cmp.answers.filter((a) => a.ready).length < 2) {
      cmp.status = 'failed';
      cmp.error = { code: 'need_two_answers', message: 'fewer than two answers to compare' };
      return;
    }
    cmp.status = 'done';
    delete cmp.error;
  }

  // look is one GET: a run's comparison waits for the run and is made on
  // the first look after it ended. The synthesis moves first, so it starts
  // on the look after the comparison ended.
  private look(cmp: FakeComparison) {
    this.synthStep(cmp);
    if (cmp.status !== 'waiting') return;
    const r = cmp.runId ? this.runs.get(cmp.runId) : undefined;
    if (r?.parts.some((p) => p.status === 'running')) return;
    this.plan(
      cmp,
      (r?.parts ?? []).map((p) => ({ id: p.agent, agent: p.agent, via: 'pc', model: p.model, text: p.status === 'done' ? p.answer : undefined })),
    );
  }

  private snapshot(cmp: FakeComparison): Comparison {
    const s: Comparison = { comparisonId: cmp.id, status: cmp.status, source: cmp.source, engine: 'pc' };
    if (cmp.runId) s.runId = cmp.runId;
    const writing = cmp.synth?.status === 'waiting' || cmp.synth?.status === 'running';
    if (cmp.status === 'waiting' || cmp.status === 'running' || writing) s.pollAfterMs = 1;
    if (cmp.error) s.error = cmp.error;
    if (cmp.synth) s.synthesis = this.synthSnap(cmp.synth);
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
      if (this.synthOn && this.badSynthesis(b.synthesis, true)) return err(400, 'bad_request');
      const listed: string[] = Array.isArray(b.judges) ? b.judges : [];
      const all = ['claude', ...this.askAgents.map((a) => a.agent)].filter((a) => this.canWrite(a));
      const writers = listed.length ? listed.filter((j) => all.includes(j)) : all;
      const pcAnswers: string[] = answers.filter((a) => a.via === 'pc').map((a) => a.agent);
      const cmp = this.newComparison({
        source: 'direct',
        key: b.clientComparisonId,
        question: String(b.question ?? ''),
        earlier: Array.isArray(b.earlier) ? b.earlier : [],
        // Who was asked: the AIs that wrote a PC answer, then the writers listed (judges).
        asked: [...new Set([...pcAnswers, ...writers])],
        synth: this.newSynth(b.synthesis, b.synthesis?.tier),
      });
      this.plan(
        cmp,
        answers.map((a) => ({ id: a.id, agent: a.agent, via: a.via, model: a.model, text: a.text })),
      );
      return json(202, this.snapshot(cmp));
    }
    const m = c.path.match(/^\/v1\/comparisons\/([^/]+)(?:\/(retry|cancel|synthesis))?$/);
    if (!m) return null;
    // An older agent has no synthesis route.
    if (m[2] === 'synthesis' && !this.synthOn) return err(404, 'not_found');
    const cmp = this.comparisons.get(decodeURIComponent(m[1]));
    if (!cmp) return err(404, 'comparison_not_found');
    const writing = cmp.synth?.status === 'waiting' || cmp.synth?.status === 'running';
    if (c.method === 'GET' && !m[2]) {
      this.look(cmp);
      return json(200, this.snapshot(cmp));
    }
    if (c.method === 'POST' && m[2] === 'synthesis') {
      const b = c.body ?? {};
      if (typeof b !== 'object' || Array.isArray(b) || this.badSynthesis({ agent: b.agent }, false)) return err(400, 'bad_request');
      if (cmp.status === 'waiting' || cmp.status === 'running' || writing) return err(409, 'comparison_running');
      // Written already: unchanged. Failed or off: written (again).
      if (cmp.synth && cmp.synth.status !== 'done') this.resetSynth(cmp.synth, b.agent ?? '');
      return json(202, this.snapshot(cmp));
    }
    if (c.method === 'POST' && m[2] === 'retry') {
      if (cmp.status === 'waiting' || cmp.status === 'running' || writing) return err(409, 'comparison_running');
      // As the agent: a comparison that failed (stopped while it waited) starts over from its run or
      // its answers, and its combined answer is written again; one that is done comes back unchanged.
      if (cmp.status === 'failed') {
        delete cmp.error;
        if (cmp.synth && cmp.synth.status !== 'off') this.resetSynth(cmp.synth);
        // A run's is made again on the next look; a direct one from the answers it has.
        if (cmp.runId) cmp.status = 'waiting';
        else this.conclude(cmp);
      }
      return json(202, this.snapshot(cmp));
    }
    if (c.method === 'POST' && m[2] === 'cancel') {
      if (cmp.status === 'waiting') {
        cmp.status = 'failed';
        cmp.error = { code: 'cancelled', message: 'cancelled' };
      }
      // A combined answer not started yet is dropped, also after the comparison ended; a running one finishes.
      if (cmp.synth?.status === 'waiting') {
        Object.assign(cmp.synth, { status: 'failed', error: { code: 'cancelled', message: 'cancelled' } });
      }
      return json(200, this.snapshot(cmp));
    }
    return err(404, 'not_found');
  }

  private runState(r: FakeRun) {
    if (r.refine?.status === 'running') r.refine = this.refined(r.refine.agent);
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
      ...(r.refine ? { refine: r.refine } : {}),
    };
  }

  // refined is the moderator's result as refineReply says.
  private refined(agent: string) {
    const base = { agent, model: 'claude-haiku-5-5', ms: 4800 };
    if (this.refineReply === 'failed') return { ...base, status: 'failed', error: { code: 'judge_bad_output' } };
    if (this.refineReply === 'open') return { ...base, status: 'done', spec: { kind: 'open', intent: 'A short poem', depth: 'normal', language: 'en' }, template: '' };
    return { ...base, status: 'done', spec: { kind: 'compare', intent: 'How the two compare', depth: 'short', language: 'en' }, template: REFINE_TEMPLATE };
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
        name: 'askorus-agent',
        version: '0.1.0',
        apiVersion: 1,
        hostname: 'DESKTOP-TEST',
        os: this.os,
        agents: [this.claudeInfo(), ...this.askAgents],
        capabilities: this.capabilities,
        reserved: [],
        ...(this.debateOn ? { debate: { maxPoints: 2, maxTurns: 4, rules: 'debate-v4' } } : {}),
        ...(this.contextOn ? { context: { maxSettled: 12, maxLastBytes: 8192 } } : {}),
        ...(this.moderatorOn ? { moderator: { maxAnswers: 6, maxAnswerBytes: 8192, rules: 'moderator-ask-v1' } } : {}),
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
        // A question to the moderator is a one-off: like the agent, its part names no session (#25).
        const sessionId = c.body.moderator ? undefined : (t.session ?? `${t.agent}:s-${this.n}`);
        // Claude's asks are Claude sessions too; they show under Ask chats.
        if (sessionId && t.agent === 'claude' && !this.sessions.some((x) => x.id === sessionId)) {
          this.sessions = [session({ id: sessionId, title: c.body.prompt, state: 'history', badges: ['ask'] }), ...this.sessions];
        }
        return { agent: t.agent, status: 'running', session: sessionId, model: t.model ?? `${t.agent}-${c.body.tier}`, ms: 1234, prompt: c.body.prompt };
      });
      const r: FakeRun = { id, tier: c.body.tier ?? 'standard', prompt: c.body.prompt, parts, key: c.body.clientRunId };
      if (c.body.refine && !c.body.refine.off) r.refine = { status: 'running', agent: c.body.refine.agent ?? parts[0]?.agent };
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
        ...(r.refine ? { refine: r.refine } : {}),
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
    return this.compareRoute(c) ?? this.debateRoute(c) ?? err(404, 'not_found');
  }
}

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function err(status: number, code: string) {
  return json(status, { error: { code, message: code } });
}
