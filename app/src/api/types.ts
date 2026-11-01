// Shapes of the PC agent's /v1 API. The agent only ever adds fields
// (extension principle 4), so every type here is a subset: unknown fields
// are ignored and unknown string values (a new state, badge or code) must
// still render.

export type SessionState = 'running' | 'stopped' | 'terminal' | 'history' | 'unknown' | (string & {});

export type Session = {
  id: string; // "claude:<uuid>"
  agent: string;
  title: string;
  name?: string;
  folder?: string;
  lastActive: string; // RFC 3339
  state: SessionState;
  activity?: string;
  remoteUrl?: string;
  model?: string;
  lastPrompt?: string;
  attachCommand?: string;
  badges?: string[]; // server | desktop | rc | …
  openModes: string[];
};

export type SessionList = {
  sessions: Session[];
  warnings: string[];
};

/** GET /v1/sessions/{id}/preview (F8): the last question and its answer. */
export type Turn = {
  role: 'user' | 'assistant' | (string & {});
  text: string;
  at?: string;
  tools?: number; // tool calls in the answer; their content stays on the PC
  /** tools by kind, most used first: read, edit, run, search, web, agent, other */
  toolKinds?: { kind: string; count: number }[];
};

export type Preview = {
  turns: Turn[];
  truncated: boolean;
};

export type SessionFilter = {
  q?: string;
  folder?: string;
  state?: string;
};

export type AdapterInfo = {
  agent: string;
  version?: string;
  available: boolean;
  capabilities: string[];
  notices?: string[];
  /** Set for AIs that can ask (Phase 2): the model of each tier. */
  ask?: { tiers: Record<string, { model: string; effort?: string }> };
  /** Set only with the capability 'judge' (P2-3): the model this AI judges with. */
  judge?: JudgeInfo;
};

/** A stable error code from the agent; message is English detail (or an AI's own words). */
export type CodeError = { code: string; message?: string };

/** The quick model an AI judges other AIs' answers with, and its company. */
export type JudgeInfo = { model: string; effort?: string; family: string };

/** How this PC compares answers (P2-3). verified and typicalMs come once the judging is measured. */
export type JudgingInfo = {
  engine: string;
  shape: string;
  labels: string;
  rules: string;
  checks: string[];
  verified?: { date: string; versions?: Record<string, string> };
  typicalMs?: number;
};

/** A place answers may go besides this PC's AIs (always [] for now). */
export type Integration = { id: string; kind: string; destination: string };

export type Info = {
  name: string;
  version: string;
  apiVersion: number;
  hostname: string;
  os: string;
  agents: AdapterInfo[];
  capabilities: string[];
  reserved: string[];
  /** Set when some AI on the PC can judge (P2-3). */
  judging?: JudgingInfo;
  integrations?: Integration[];
};

export type ServeStatus = {
  state: string; // off | starting | running | failed | untrusted | external
  since?: string;
  environmentId?: string;
  sessions: number;
  capacity?: number;
  restarts?: number;
  error?: string;
};

export type Folder = {
  id: string;
  name: string;
  path: string;
  server: { enabled: boolean; spawn?: string; capacity?: number };
  status?: ServeStatus;
  isGit: boolean;
  /** "config": allowed on the PC; "history": has past sessions. Older agents leave it out. */
  source?: 'config' | 'history' | (string & {});
  lastActive?: string;
};

export type FolderList = {
  folders: Folder[];
};

export type StartRequest = {
  agent?: string;
  folder: string; // folder id
  name: string;
  model?: string;
  effort?: string;
};

export type OpenResult = {
  session: Session;
  remoteUrl?: string;
  action: 'none' | 'resumed' | 'started' | (string & {});
  hint?: 'use_rc_in_terminal' | 'server_session' | (string & {});
};

export type JobStarted = {
  jobId: string;
  kind: string;
  status: 'running';
  pollAfterMs: number;
};

export type Job = {
  jobId: string;
  kind: string;
  status: 'running' | 'done' | 'failed' | (string & {});
  pollAfterMs?: number;
  result?: OpenResult;
  error?: { code: string; message: string };
};

export type PairStarted = {
  requestId: string;
  status: 'pending';
  pollAfterMs: number;
};

export type PairStatus = {
  status: 'pending' | 'approved' | (string & {});
  pollAfterMs?: number;
  token?: string;
  deviceId?: string;
  deviceName?: string;
  apiVersion: number;
};

export type Paired = {
  token: string;
  deviceId: string;
  deviceName: string;
  apiVersion: number;
};

/** Ask tiers: 빠르게 · 기본 · 깊게 (Phase 2). */
export const tiers = ['quick', 'standard', 'deep'] as const;
export type Tier = (typeof tiers)[number];

/** One AI of a run. `session` ("<agent>:<id>") carries an earlier answer on. */
export type RunTarget = { agent: string; model?: string; effort?: string; session?: string };

/** POST /v1/runs (mode ask, P2-1). */
export type RunRequest = {
  mode: 'ask';
  prompt: string;
  tier?: Tier;
  agents?: RunTarget[];
  /** The phone's own id for the run, so a retried request does not ask twice. */
  clientRunId?: string;
  /** Compare the answers once they are in (P2-3). */
  compare?: RunCompare;
};

/**
 * earlier: questions the judges' companies already got (oldest first);
 * judges: who may judge; synthesis: whether one AI then writes the answers
 * into one (S5b). Without synthesis the PC writes none (an S5 app).
 */
export type RunCompare = { earlier?: string[]; judges?: string[]; synthesis?: SynthesisRequest };

/**
 * The combined answer a comparison asks for (P2-3 S5b): agent is the AI the
 * person picked (the PC falls back to the first asked AI that may write;
 * left out, the PC picks), or off. The tier is the run's.
 */
export type SynthesisRequest = { agent?: string; off?: boolean };

/** One AI's share of a run; it shows as soon as that AI is done. */
export type RunPart = {
  agent: string;
  status: 'running' | 'done' | 'failed' | (string & {});
  session?: string;
  model?: string;
  answer?: string;
  ms?: number;
  inputTokens?: number;
  outputTokens?: number;
  error?: { code: string; message?: string };
};

export type Run = {
  runId: string;
  mode: string;
  tier?: string;
  status: 'running' | 'done' | 'failed' | (string & {});
  startedAt: string;
  finishedAt?: string;
  parts: RunPart[];
  pollAfterMs?: number;
  /** The comparison of this run's answers (P2-3), on the 202 and every GET. */
  comparisonId?: string;
  /** Why the run's compare was not taken (only on the POST's 202); the ask goes on. */
  compareError?: CodeError;
};

// Answer comparison (P2-3): GET /v1/comparisons/{id}. Every offset is in
// UTF-16 code units (JavaScript string indexes).

/** Math in a unit's text, marks included: the only places the phone draws math. */
export type UnitMath = { start: number; end: number; display: boolean };

/** One sentence, list item, table row or quote of an answer. start/end are offsets in the answer. */
export type Unit = {
  id: string; // "claude:3"
  n: number;
  kind: 'sentence' | 'item' | 'row' | 'quote' | (string & {});
  text: string;
  context?: string;
  start: number;
  end: number;
  judged: boolean;
  math?: UnitMath[];
};

export type CompareAnswer = {
  id: string;
  agent: string;
  via: 'pc' | 'handoff' | (string & {});
  family: string;
  model?: string;
  status: 'ready' | 'missing' | (string & {});
  notJudged: number;
  units: Unit[];
};

/** One judge reading the units of answer `from` against answer `against`. judge '' is a no_judge record. */
export type JudgeRequest = {
  id: string;
  from: string;
  against: string;
  judge: string;
  judgeOnly: boolean;
  model?: string;
  status: 'queued' | 'running' | 'done' | 'failed' | (string & {});
  attempt: number;
  ms?: number;
  inputTokens?: number;
  outputTokens?: number;
  error?: CodeError;
};

export type Judgment = {
  unit: string;
  against: string;
  request: string;
  judge: string;
  core: boolean;
  label: 'same' | 'opposite' | 'absent' | (string & {});
  evidence: string[];
  unsure: boolean;
  flags: string[];
};

/** Digits to show bold: offsets into the unit's text. */
export type NumberSpan = { unit: string; start: number; end: number };

/** One side of a card item: the answers (ids) that say it, and their units (the first is shown). */
export type Position = { agents: string[]; units: string[]; numberSpans?: NumberSpan[] };

export type CardItem = {
  key: string; // stays the same while the comparison runs
  kind: 'differ' | 'check' | 'agree' | 'onlyOne' | (string & {});
  positions: Position[];
  numbersDiffer?: boolean;
  check: string[];
  missing: string[];
};

export type CardCounts = { differ: number; check: number; agree: number; onlyOne: number; notCompared: number; notJudged: number };

/**
 * A comparison's combined answer (S5b), written by one AI once the judging
 * ended. Its status is its own: the comparison's status stays the judges'.
 * agent is the AI asked for until one is picked, then the writer; ms and
 * tokens add up over the attempts; text (Markdown) comes only when done;
 * judged: the writer had the comparison's results; unnamedSides: Differ
 * sides whose AI names the text never mentions.
 */
export type Synthesis = {
  status: 'waiting' | 'running' | 'done' | 'failed' | 'off' | (string & {});
  agent?: string;
  model?: string;
  attempt?: number;
  ms?: number;
  inputTokens?: number;
  outputTokens?: number;
  text?: string;
  judged: boolean;
  unnamedSides?: number;
  error?: CodeError;
};

export type Comparison = {
  comparisonId: string;
  /** The judges' status; pollAfterMs also stays while the synthesis waits or writes. */
  status: 'waiting' | 'running' | 'done' | 'failed' | (string & {});
  pollAfterMs?: number;
  source: 'run' | 'direct' | (string & {});
  runId?: string;
  engine: string;
  answers: CompareAnswer[];
  requests: JudgeRequest[];
  judgments: Judgment[];
  items: CardItem[];
  counts: CardCounts;
  error?: CodeError;
  /** Set by agents that write combined answers (S5b); an older agent leaves it out. */
  synthesis?: Synthesis;
};

/** An answer the phone sends for a direct comparison (POST /v1/comparisons). */
export type ComparisonAnswer = { id: string; agent: string; via: 'pc' | 'handoff'; model?: string; text: string };

export type ComparisonRequest = {
  /** The phone's own id, so a retried request makes one comparison. */
  clientComparisonId?: string;
  question: string;
  earlier?: string[];
  answers: ComparisonAnswer[];
  judges?: string[];
  /** The combined answer (S5b), with the question's tier ('' or left out: standard). */
  synthesis?: SynthesisRequest & { tier?: Tier };
};
