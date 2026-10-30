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
};

export type Info = {
  name: string;
  version: string;
  apiVersion: number;
  hostname: string;
  os: string;
  agents: AdapterInfo[];
  capabilities: string[];
  reserved: string[];
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
