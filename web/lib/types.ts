// Shapes of the Orla API. docs/events.md and the Go handlers are the source of truth.

export interface OrlaEvent {
  id: string;
  /** Per session, from 1, without gaps. */
  seq: number;
  time: string;
  session: string;
  run?: string;
  parent_run?: string;
  source?: { engine?: string; agent?: string; native_session?: string };
  type: string;
  data?: Record<string, unknown>;
}

export type RunStatus = 'running' | 'done' | 'failed' | 'cancelled';

export interface Run {
  id: string;
  session: string;
  prompt: string;
  status: RunStatus;
  started: string;
  finished?: string;
}

/** What a session is doing, as the server derives it. */
export type SessionState = 'working' | 'waiting' | 'idle' | 'ended';

export interface Session {
  id: string;
  agent: string;
  cwd: string;
  native_session?: string;
  terminal?: string;
  created: string;
  active_run?: string;
  last_seq: number;
  state: SessionState;
  pending: number;
  updated?: string;
  last_run?: Run;
}

export interface PendingApproval {
  approval_id: string;
  session: string;
  run: string;
  tool: string;
  input: unknown;
  call_id?: string;
  terminal?: string;
  asked_at: string;
  expires_at: string;
}

export interface TerminalInfo {
  id: string;
  argv: string[];
  cwd: string;
  started: string;
  clients: number;
}
