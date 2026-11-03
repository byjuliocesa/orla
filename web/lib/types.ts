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
  /** Set when the session works in a worktree Orla made. */
  worktree?: Worktree;
}

export interface Worktree {
  path: string;
  /** The repository's main work tree. */
  repo: string;
  branch: string;
  /** The branch (or commit) it started from and merges back into. */
  base: string;
  created: string;
  removed?: string;
  /** On removal: the stash that holds what was uncommitted. */
  stash?: string;
  /** On removal: the branch stayed, because its base lacked its commits. */
  branch_kept?: boolean;
}

export interface Repo {
  root: string;
  main: string;
  branch?: string;
  head?: string;
}

export type FileStatus = 'added' | 'modified' | 'deleted' | 'renamed' | 'untracked';

export interface ChangedFile {
  path: string;
  /** The old path of a rename. */
  from?: string;
  status: FileStatus;
  added: number;
  removed: number;
  binary?: boolean;
  /** Part of the change is not committed yet, so it can be discarded. */
  uncommitted: boolean;
}

/** What a session changed: against its worktree's base, or against the last commit. */
export type Changes =
  | { git: false; folder_gone?: boolean }
  | {
      git: true;
      repo: Repo;
      files: ChangedFile[];
      more: number;
      worktree?: Worktree;
      base_commit?: string;
      ahead?: number;
      behind?: number;
    };

export interface MergeState {
  preview: {
    ahead: number;
    behind: number;
    clean: boolean;
    conflicts?: string[];
    /** The work tree that has the base checked out, where the merge runs. */
    into?: string;
  };
  base: string;
  branch: string;
  /** Files not committed in the worktree, which a merge leaves behind. */
  uncommitted: number;
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
