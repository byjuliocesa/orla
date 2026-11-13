import type { Automation, AutomationLine, ChangedFile, LibraryStatus, SavedSession, Changes, Flow, FlowStep, MergeState, OrlaEvent, PendingApproval, Repo, Session, TerminalInfo, Worktree } from './types';

/** The browser is not logged in, or its login ended. */
export class Unauthorized extends Error {
  constructor() {
    super('not logged in');
  }
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

/** The SSH key the library reaches its repository with; `own` when it is the owner's. */
export interface LibraryKey {
  key: string;
  own: boolean;
}

/** Claude Code's /context: what fills a conversation's window. */
export interface ContextPart {
  name: string;
  tokens: number;
  percent?: number;
  detail?: string;
}
export interface ContextUsage {
  model: string;
  used: number;
  total: number;
  categories: ContextPart[];
  mcp_tools?: ContextPart[];
  skills?: ContextPart[];
  memory?: ContextPart[];
}

/** The account's plan windows, as the last run reported them. */
export interface PlanLimits {
  limits: Record<string, { used: number; resets_at?: number }> | null;
  at?: string;
}

/** A file the owner attached, as the server keeps it. */
export interface Upload {
  id: string;
  name: string;
  type: string;
  size: number;
  path: string;
}

/** Sends one file, reporting progress from 0 to 1. Aborting stops it. */
export function uploadFile(file: File, onProgress: (share: number) => void): { done: Promise<Upload>; abort: () => void } {
  const xhr = new XMLHttpRequest();
  const done = new Promise<Upload>((resolve, reject) => {
    xhr.open('POST', '/v1/uploads');
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress(e.loaded / e.total);
    xhr.onload = () => {
      if (xhr.status === 401) return reject(new Unauthorized());
      if (xhr.status !== 201) {
        let message = xhr.responseText.trim() || xhr.statusText;
        try {
          message = (JSON.parse(xhr.responseText) as { error?: string }).error ?? message;
        } catch {}
        return reject(new ApiError(xhr.status, message));
      }
      resolve(JSON.parse(xhr.responseText) as Upload);
    };
    xhr.onerror = () => reject(new ApiError(0, 'The upload did not reach Orla.'));
    xhr.onabort = () => reject(new ApiError(0, 'aborted'));
    const form = new FormData();
    form.append('file', file);
    xhr.send(form);
  });
  return { done, abort: () => xhr.abort() };
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) throw new Unauthorized();
  if (!res.ok) {
    const text = await res.text();
    let message = text.trim() || res.statusText;
    try {
      message = (JSON.parse(text) as { error?: string }).error ?? message;
    } catch {
      // plain text error
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export interface RunOptions {
  prompt: string;
  permission_mode?: string;
  model?: string;
  effort?: string;
  ultracode?: boolean;
  /** Upload ids. */
  attachments?: string[];
}

export const api = {
  login: (token: string) => call<void>('POST', '/auth/login', { token }),
  logout: () => call<void>('POST', '/auth/logout'),

  limits: () => call<PlanLimits>('GET', '/v1/limits'),
  context: (session: string) => call<{ context: ContextUsage | null }>('GET', `/v1/sessions/${session}/context`).then((r) => r.context),
  files: (cwd: string, q: string) => call<{ files: { path: string; dir?: boolean }[] }>('GET', `/v1/files?cwd=${encodeURIComponent(cwd)}&q=${encodeURIComponent(q)}`).then((r) => r.files),
  history: (session: string) => call<{ events: OrlaEvent[] }>('GET', `/v1/sessions/${session}/history`).then((r) => r.events ?? []),
  sessionDetail: (session: string) => call<{ session: Session; remote?: string }>('GET', `/v1/sessions/${session}`),
  flows: (cwd: string) => call<{ flows: Flow[] }>('GET', `/v1/flows?cwd=${encodeURIComponent(cwd)}`),
  flowAction: (action: 'archive' | 'restore' | 'delete' | 'duplicate', flow: string, step: string | undefined, cwd: string) =>
    call<{ ok: boolean; name?: string }>(
      action === 'delete' ? 'DELETE' : 'POST',
      `/v1/flows/${encodeURIComponent(flow)}${step ? `/steps/${encodeURIComponent(step)}` : ''}${action === 'delete' ? '' : `/${action}`}?cwd=${encodeURIComponent(cwd)}`,
    ),
  automations: () => call<{ automations: Automation[] }>('GET', '/v1/automations'),
  automationAction: (name: string, action: 'start' | 'stop', session?: string) =>
    call<{ ok: boolean }>('POST', `/v1/automations/${encodeURIComponent(name)}/${action}${session ? `?session=${encodeURIComponent(session)}` : ''}`),
  automationLogs: (name: string, session: string | undefined, after: number) =>
    call<{ lines: AutomationLine[] }>('GET', `/v1/automations/${encodeURIComponent(name)}/logs?after=${after}${session ? `&session=${encodeURIComponent(session)}` : ''}`),
  deleteAutomation: (name: string) => call<{ ok: boolean }>('DELETE', `/v1/automations/${encodeURIComponent(name)}`),
  archivedFlows: () => call<{ flows: Flow[]; steps: { flow: string; step: FlowStep; path: string }[] }>('GET', '/v1/flows/archived'),
  skillDoc: (name: string, cwd: string) => call<{ name: string; path: string; body: string }>('GET', `/v1/skill-doc?name=${encodeURIComponent(name)}&cwd=${encodeURIComponent(cwd)}`),
  library: () => call<LibraryStatus>('GET', '/v1/library'),
  libraryKey: () => call<LibraryKey>('GET', '/v1/library/key'),
  setLibraryKey: (privateKey: string) => call<LibraryKey>('PUT', '/v1/library/key', { private: privateKey }),
  removeLibraryKey: () => call<LibraryKey>('DELETE', '/v1/library/key'),
  libraryAuto: (auto: boolean) => call<LibraryStatus>('PATCH', '/v1/library', { auto }),
  libraryAction: (action: 'connect' | 'sync' | 'disconnect' | 'adopt' | 'move', body?: { remote?: string; from?: string; name?: string; to?: string }) =>
    call<LibraryStatus>('POST', `/v1/library/${action}`, body),
  saveSession: (session: string) => call<SavedSession>('POST', `/v1/sessions/${session}/save`),
  openSaved: (key: string, cwd: string) => call<Session>('POST', '/v1/library/open', { key, cwd }),
  pref: <T,>(key: string) => call<{ value: T | null }>('GET', `/v1/prefs/${key}`),
  setPref: (key: string, value: unknown) => call<{ value: unknown }>('PUT', `/v1/prefs/${key}`, value),
  skills: (cwd: string) => call<{ skills: { name: string; description?: string; scope: string; tokens: number; flow?: boolean; orla?: boolean }[] }>('GET', `/v1/skills?cwd=${encodeURIComponent(cwd)}`),
  forkSession: (session: string, run?: string) => call<Session>('POST', `/v1/sessions/${session}/fork`, run ? { run } : undefined),
  sessions: () => call<{ sessions: Session[] }>('GET', '/v1/sessions').then((r) => r.sessions),
  createSession: (cwd: string, agent = 'claude') =>
    call<Session>('POST', '/v1/sessions', { cwd, agent }),
  startRun: (session: string, opts: RunOptions) =>
    call<{ id: string }>('POST', `/v1/sessions/${session}/runs`, opts),
  updateSession: (session: string, change: { title?: string; pinned?: boolean; archived?: boolean ;  skills_off?: string[]; flows?: string[] }) =>
    call<Session>('PATCH', `/v1/sessions/${session}`, change),
  deleteSession: (session: string) => call<void>('DELETE', `/v1/sessions/${session}`),
  cancel: (session: string) => call<void>('POST', `/v1/sessions/${session}/cancel`),

  approvals: () =>
    call<{ approvals: PendingApproval[] }>('GET', '/v1/approvals').then((r) => r.approvals),
  resolve: (session: string, approval: string, allow: boolean, message?: string) =>
    call<void>('POST', `/v1/sessions/${session}/approvals/${approval}`, {
      allow,
      message: message || undefined,
      by: 'web',
    }),

  gitInfo: (path: string) =>
    call<{ git: boolean; repo?: Repo }>('GET', `/v1/git?path=${encodeURIComponent(path)}`),
  createWorktree: (from: string, branch?: string) =>
    call<{ worktree: Worktree; cwd: string }>('POST', '/v1/worktrees', { from, branch: branch || undefined }),
  changes: (session: string) => call<Changes>('GET', `/v1/sessions/${session}/changes`),
  diff: (session: string, path: string) =>
    call<{ file: ChangedFile; patch: string; truncated: boolean }>(
      'GET',
      `/v1/sessions/${session}/diff?path=${encodeURIComponent(path)}`,
    ),
  discard: (session: string, path: string) =>
    call<{ stash: string }>('POST', `/v1/sessions/${session}/discard`, { path }),
  commit: (session: string, message: string) =>
    call<{ commit: string }>('POST', `/v1/sessions/${session}/commit`, { message }),
  mergeState: (session: string) => call<MergeState>('GET', `/v1/sessions/${session}/merge`),
  merge: (session: string) => call<MergeState>('POST', `/v1/sessions/${session}/merge`),
  removeWorktree: (session: string) =>
    call<{ removal: { stash?: string; branch_kept: boolean }; branch: string }>(
      'DELETE',
      `/v1/sessions/${session}/worktree`,
    ),

  terminals: () =>
    call<{ terminals: TerminalInfo[] }>('GET', '/v1/terminals').then((r) => r.terminals),
  openTerminal: (spec: { argv?: string[]; cwd?: string; cols?: number; rows?: number }) =>
    call<TerminalInfo>('POST', '/v1/terminals', spec),
  closeTerminal: (id: string) => call<void>('DELETE', `/v1/terminals/${id}`),
};
