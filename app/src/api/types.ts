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
  /**
   * Set for AIs that can ask (Phase 2): the model of each tier, and (11-06,
   * newer agents) the efforts each of those models may take, least to most.
   */
  ask?: { tiers: Record<string, { model: string; effort?: string }>; efforts?: Record<string, string[]> };
};

/** A stable error code from the agent; message is English detail (or an AI's own words). */
export type CodeError = { code: string; message?: string };

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
  integrations?: Integration[];
  /** The agent runs debates (P2-5, #26); an older agent leaves it out, and the phone offers none. */
  debate?: DebateInfo;
  /** The agent takes a conversation record (#29); an older agent leaves it out, and the phone sends none. */
  context?: ContextInfo;
  /** The agent asks the moderator alone (#25); an older agent leaves it out, and the phone offers no '사회자'. */
  moderator?: ModeratorInfo;
  /** The agent lets a paired phone answer a pairing request (#58); an older agent leaves it out. */
  pairing?: { approve: boolean };
};

/** /v1/hello: what a phone of the same Tailscale account may know of a PC before it is paired (#58). */
export type Hello = {
  name: string;
  version: string;
  apiVersion: number;
  hostname: string;
  os: string;
  /** 'window': a pairing window is open on the PC and takes the phone at once. 'ask': someone is asked. */
  pairing: 'window' | 'ask' | (string & {});
};

/** A pairing request without a code that waits for an answer, as a paired phone sees it (#58). */
export type PairQuestion = { id: string; deviceName: string; match: string; at: string };

/** /v1/info's moderator: the limits of the answers a moderator ask carries. */
export type ModeratorInfo = { maxAnswers: number; maxAnswerBytes: number; rules: string };

/**
 * A question to the moderator (#25): the moderator AI (its model and effort
 * as set for it), and the previous turn's answers by AI it reads.
 */
export type RunModerator = { agent: string; model?: string; effort?: string; answers?: { agent: string; text: string }[] };

/** /v1/info's context: the record's limits. */
export type ContextInfo = { maxSettled: number; maxLastBytes: number };

/** A point an earlier debate of the conversation closed (#29): its topic, result and the moderator's line. */
export type Settled = { topic: string; result: string; line: string };

/**
 * The conversation record a follow-up, a comparison made again and a debate
 * carry (#29): the latest earlier questions (oldest first; the last is the
 * previous turn's), the points earlier debates settled, and the previous
 * turn's final answer.
 */
export type RequestContext = { earlier?: string[]; settled?: Settled[]; last?: string };

/** /v1/info's debate: its limits and the prompts' version. */
export type DebateInfo = { maxPoints: number; maxTurns: number; rules: string };

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
  /** Refine the question first (질문 정제, milestone C); left out is off (an older agent ignores it). */
  refine?: RefineRequest;
  /** The conversation record (#29), only to an agent whose info has context. */
  context?: RequestContext;
  /** Ask the moderator alone (#25): agents, compare and refine are then not read. */
  moderator?: RunModerator;
};

/**
 * The moderator a run asks for (milestone C): agent is the AI the person
 * picked as moderator (the PC falls back to the first asked AI that may;
 * left out, the PC picks).
 */
export type RefineRequest = { agent?: string; off?: boolean };

/** The moderator's reading of a question (refine-v1): kind and depth from fixed lists. */
export type QuestionSpec = { kind: string; intent: string; depth: string; language: string };

/**
 * A run's refining (milestone C): running until the moderator's spec is in;
 * done with the spec and the template every AI got after the question
 * ("" for a kind without one); failed when the question went as written.
 */
export type Refine = {
  status: 'running' | 'done' | 'failed' | (string & {});
  agent?: string;
  model?: string;
  ms?: number;
  spec?: QuestionSpec;
  template?: string;
  error?: CodeError;
};

/**
 * The comparison a run asks for once its answers are in (P2-3; since C6
 * only the moderator's combined answer). judges: the asked AIs that may
 * write it, by the name of P2-3's judges (a writer among them gets
 * earlier, the questions its company already got, oldest first);
 * synthesis: whether the moderator then writes the answers into one (S5b).
 * Without synthesis the PC writes none (an S5 app).
 */
export type RunCompare = { earlier?: string[]; judges?: string[]; synthesis?: SynthesisRequest };

/**
 * The combined answer a comparison asks for (P2-3 S5b): agent is the AI the
 * person picked (the PC falls back to the first asked AI that may write;
 * left out, the PC picks), or off. The tier is the run's. model and effort
 * (11-06) are the moderator model set for agent in Settings; the PC uses
 * them when that AI writes and they are one of its tier models, else the
 * tier's.
 */
export type SynthesisRequest = { agent?: string; off?: boolean; model?: string; effort?: string };

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
  /** The question's refining, when the run asked for it (milestone C). */
  refine?: Refine;
};

// Answer comparison (P2-3): GET /v1/comparisons/{id}. Since C6 (#19,
// 2026-11-05) a comparison is only the moderator's combined answer: the PC
// judges no more. Older agents and conversations kept before C6 also carry
// the judging's answers, requests, judgments, items and counts, and a
// synthesis with judged, unnamedSides and topics: the phone reads none of
// them, so they are left out here (JSON keeps them; nothing breaks).

/**
 * A comparison's combined answer (S5b), written by the moderator once the
 * answers are in. Its status is its own: pollAfterMs stays while it waits
 * or writes. agent is the AI asked for until one is picked, then the
 * writer; ms and tokens add up over the attempts; text (Markdown) comes
 * only when done.
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
  error?: CodeError;
};

export type Comparison = {
  comparisonId: string;
  /** The comparison's own status (done at once since C6); pollAfterMs also stays while the synthesis waits or writes. */
  status: 'waiting' | 'running' | 'done' | 'failed' | (string & {});
  pollAfterMs?: number;
  source: 'run' | 'direct' | (string & {});
  runId?: string;
  engine: string;
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
  /** The asked AIs that may write the combined answer (RunCompare.judges). */
  judges?: string[];
  /** The combined answer (S5b), with the question's tier ('' or left out: standard). */
  synthesis?: SynthesisRequest & { tier?: Tier };
  /** The conversation record the turn was sent with (#29). */
  context?: RequestContext;
};

// Debates (토론, P2-5, #26): POST /v1/debates, GET /v1/debates/{id},
// POST /v1/debates/{id}/stop. AIs are named by their agent codes.

/** A debate's status; the phone polls while pollAfterMs is there. */
export type DebateStatus = 'planning' | 'debating' | 'writing' | 'done' | 'stopped' | 'failed' | (string & {});

/** One debater's turn: the moderator's question and the reply (ms runs on while it runs). */
export type DebateTurn = {
  ai: string;
  ask: string;
  status: 'running' | 'done' | 'failed' | (string & {});
  text?: string;
  stance?: 'keep' | 'change' | 'partly' | (string & {});
  model?: string;
  ms: number;
  error?: CodeError;
};

/** One point where the answers differ; one that is not debated has no turns. */
export type DebatePoint = {
  id: string;
  topic: string;
  nature: 'fact' | 'reasoning' | 'judgment' | 'taste' | (string & {});
  debate: boolean;
  goal?: string;
  sides: { ais: string[]; says: string }[];
  turns: DebateTurn[];
  result?: 'agreed' | 'conditional' | 'differ' | 'kept' | (string & {});
  line?: string;
  checking?: boolean;
};

export type Debate = {
  debateId: string;
  status: DebateStatus;
  pollAfterMs?: number;
  moderator?: string;
  model?: string;
  tier?: string;
  points: DebatePoint[];
  /** The answer after the debate (토론 뒤 정리), Markdown. */
  after?: { text: string; agent: string; model?: string; ms: number };
  calls: number;
  ms: number;
  error?: CodeError;
};

/** An answer a debate starts from: via pc (it speaks) or handoff (a side only). */
export type DebateAnswer = { agent: string; via: 'pc' | 'handoff'; model?: string; text: string };

export type DebateRequest = {
  /** The phone's own id, so a retried request makes one debate. */
  clientDebateId?: string;
  question: string;
  earlier?: string[];
  /** The AIs the earlier questions may go to (as a comparison's judges). */
  judges?: string[];
  answers: DebateAnswer[];
  /** The combined answer's text the debate starts from. */
  synthesis: string;
  moderator?: string;
  tier?: Tier;
  /** The moderator model set for moderator (11-06), as SynthesisRequest's model and effort. */
  moderatorModel?: string;
  moderatorEffort?: string;
  /** The conversation record the turn was sent with (#29), for the plan and the answer after it. */
  context?: RequestContext;
};
