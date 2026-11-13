// Shapes of the Orla API. The Go handlers and internal/events are the source of truth.

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
  /** The owner's name for it, in place of the folder. */
  title?: string;
  pinned?: boolean;
  archived?: string;
  fork_parent?: string;
  fork_run?: string;
  /** Skills this session leaves out of its conversation. */
  skills_off?: string[];
  /** Flow skills this session follows; every other flow is off. */
  flows?: string[];
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
  /** origin's page on its forge, when it has one. */
  web?: string;
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

/** A flow: a skill that says, step by step, how a kind of task is done. */
export interface FlowStep {
  id: string;
  title: string;
  uses: string[];
  signals: string[];
  branches: { to: string; when?: string }[];
  /** Where the step goes when done, when not the step below it: a step id, or "end". */
  next?: string;
  file?: string;
  file_body?: string;
  body: string;
}
export interface Flow {
  name: string;
  title: string;
  description: string;
  scope: string;
  dir: string;
  path: string;
  intro?: string;
  steps: FlowStep[];
  files: string[];
  modified: string;
}

/** An automation: a script Orla runs and watches, so routine work costs no model tokens. */
export interface AutomationStatus {
  name: string;
  session?: string;
  state: 'running' | 'waiting' | 'stopped' | 'failed';
  since: string;
  last_run?: string;
  last_end?: string;
  exit?: number;
  runs: number;
  error?: string;
}
export interface Automation {
  name: string;
  title: string;
  description: string;
  run: string;
  scope: 'session' | 'global';
  trigger: { kind: string; every?: number; on?: string; text: string };
  restart: string;
  dir: string;
  path: string;
  body?: string;
  files: string[];
  problem?: string;
  modified: string;
  instances: AutomationStatus[];
  /** Where the owner turned it on: "" for global, else session ids. */
  on: string[];
}
export interface AutomationLine {
  n: number;
  time: string;
  stream: 'out' | 'err' | 'orla';
  text: string;
}

/** One skill, flow or automation in the library, or one an agent loads from outside it. */
export interface LibraryEntry {
  name: string;
  description?: string;
  kind: 'skill' | 'flow' | 'automation' | 'system';
  /** shared, an agent's name, or automations. */
  place: string;
  linked?: string[];
  /** Agents where a folder of the same name keeps it from being linked. */
  blocked?: string[];
}

export interface LibraryAgent {
  name: string;
  title: string;
  dir: string;
  present: boolean;
}

/** The library and how it stands with its repository. */
export interface LibraryStatus {
  dir: string;
  remote: string;
  branch?: string;
  auto: boolean;
  state: 'off' | 'synced' | 'pending' | 'syncing' | 'error' | 'conflict';
  error?: string;
  conflicts?: string[];
  last_sync?: string;
  pending: number;
  entries: LibraryEntry[];
  outside: LibraryEntry[];
  agents: LibraryAgent[];
}
