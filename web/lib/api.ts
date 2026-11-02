import type { PendingApproval, Session, TerminalInfo } from './types';

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
}

export const api = {
  login: (token: string) => call<void>('POST', '/auth/login', { token }),
  logout: () => call<void>('POST', '/auth/logout'),

  sessions: () => call<{ sessions: Session[] }>('GET', '/v1/sessions').then((r) => r.sessions),
  createSession: (cwd: string, agent = 'claude') =>
    call<Session>('POST', '/v1/sessions', { cwd, agent }),
  startRun: (session: string, opts: RunOptions) =>
    call<{ id: string }>('POST', `/v1/sessions/${session}/runs`, opts),
  cancel: (session: string) => call<void>('POST', `/v1/sessions/${session}/cancel`),

  approvals: () =>
    call<{ approvals: PendingApproval[] }>('GET', '/v1/approvals').then((r) => r.approvals),
  resolve: (session: string, approval: string, allow: boolean, message?: string) =>
    call<void>('POST', `/v1/sessions/${session}/approvals/${approval}`, {
      allow,
      message: message || undefined,
      by: 'web',
    }),

  terminals: () =>
    call<{ terminals: TerminalInfo[] }>('GET', '/v1/terminals').then((r) => r.terminals),
  openTerminal: (spec: { argv?: string[]; cwd?: string; cols?: number; rows?: number }) =>
    call<TerminalInfo>('POST', '/v1/terminals', spec),
  closeTerminal: (id: string) => call<void>('DELETE', `/v1/terminals/${id}`),
};
