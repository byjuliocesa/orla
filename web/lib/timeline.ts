import type { OrlaEvent, RunStatus } from './types';

// Turns a session's events into what the screen shows: runs, and in each run its steps, with
// every tool call paired to its result and to the approval asked for it.

export interface Approval {
  id: string;
  tool: string;
  input: unknown;
  callId?: string;
  /** Set when the CLI runs in an Orla terminal, which asks the same question. */
  terminal?: string;
  expiresAt?: string;
  askedAt: string;
  /** Absent while the request waits. */
  outcome?: {
    allowed: boolean;
    by: string;
    message?: string;
    /** Nobody decided through Orla; the terminal still asks. */
    withdrawn: boolean;
    at: string;
  };
}

export interface ToolStep {
  kind: 'tool';
  /** Seq of the event that made the step, to tell steps that arrived live. */
  seq: number;
  callId: string;
  name: string;
  input: unknown;
  at: string;
  result?: { ok: boolean; output: string; truncated: boolean; interrupted: boolean; at: string };
  approval?: Approval;
  /** Steps of a subagent this call started. */
  children: Step[];
}

export interface MessageStep {
  kind: 'message';
  seq: number;
  text: string;
  at: string;
}

export interface ApprovalStep {
  kind: 'approval';
  seq: number;
  approval: Approval;
  at: string;
}

export interface ErrorStep {
  kind: 'error';
  seq: number;
  message: string;
  at: string;
}

export type Step = ToolStep | MessageStep | ApprovalStep | ErrorStep;

export interface Tokens {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface RunView {
  id: string;
  prompt: string;
  cwd?: string;
  interactive: boolean;
  terminal?: string;
  permissionMode?: string;
  /** The model that answered, from the responses' usage. */
  model?: string;
  started: string;
  finished?: string;
  status: RunStatus;
  summary?: string;
  steps: Step[];
  /** Summed from each model response while the run goes on. */
  live: Tokens;
  /** The CLI's own total, once the run reports it. */
  usage?: Tokens & { turns: number; durationMs?: number; costUsd?: number };
}

export interface Limits {
  [window: string]: { used: number; resets_at?: number };
}

export interface Timeline {
  runs: RunView[];
  pending: Approval[];
  status?: { state: string; message?: string; at: string };
  limits?: Limits;
  lastSeq: number;
}

const zero = (): Tokens => ({ input: 0, output: 0, cacheRead: 0, cacheWrite: 0 });

function str(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

function num(v: unknown): number {
  return typeof v === 'number' ? v : 0;
}

export function buildTimeline(events: readonly OrlaEvent[]): Timeline {
  const runs: RunView[] = [];
  const runById = new Map<string, RunView>();
  const calls = new Map<string, ToolStep>();
  const approvals = new Map<string, Approval>();
  const t: Timeline = { runs, pending: [], lastSeq: 0 };

  const runOf = (e: OrlaEvent): RunView | undefined => {
    if (!e.run) return undefined;
    let r = runById.get(e.run);
    if (!r) {
      // Events of a run whose start was not seen (hooks installed mid-prompt).
      r = { id: e.run, prompt: '', interactive: false, started: e.time, status: 'running', steps: [], live: zero() };
      runById.set(e.run, r);
      runs.push(r);
    }
    return r;
  };

  // A subagent's steps go under the call that started it, when that call is known.
  const place = (r: RunView, d: Record<string, unknown>, step: Step) => {
    const parent = calls.get(str(d.parent_call));
    (parent ? parent.children : r.steps).push(step);
  };

  for (const e of events) {
    t.lastSeq = Math.max(t.lastSeq, e.seq);
    const d = e.data ?? {};
    switch (e.type) {
      case 'run.started': {
        const r = runOf(e)!;
        r.prompt = str(d.prompt);
        r.cwd = str(d.cwd) || undefined;
        r.interactive = d.interactive === true;
        r.terminal = str(d.terminal) || undefined;
        r.permissionMode = str(d.permission_mode) || undefined;
        r.started = e.time;
        break;
      }
      case 'run.finished': {
        const r = runOf(e);
        if (!r) break;
        r.status = (str(d.status) || 'done') as RunStatus;
        r.summary = str(d.summary) || undefined;
        r.finished = e.time;
        break;
      }
      case 'message.assistant': {
        const r = runOf(e);
        if (r && str(d.text).trim()) place(r, d, { kind: 'message', seq: e.seq, text: str(d.text), at: e.time });
        break;
      }
      case 'tool.call': {
        const r = runOf(e);
        if (!r) break;
        const step: ToolStep = {
          kind: 'tool',
          seq: e.seq,
          callId: str(d.call_id),
          name: str(d.name),
          input: d.input,
          at: e.time,
          children: [],
        };
        if (step.callId) calls.set(step.callId, step);
        place(r, d, step);
        break;
      }
      case 'tool.result': {
        const step = calls.get(str(d.call_id));
        if (!step) break;
        step.result = {
          ok: d.ok === true,
          output: str(d.output),
          truncated: d.truncated === true,
          interrupted: d.interrupted === true,
          at: e.time,
        };
        break;
      }
      case 'approval.requested': {
        const a: Approval = {
          id: str(d.approval_id),
          tool: str(d.tool),
          input: d.input,
          callId: str(d.call_id) || undefined,
          terminal: str(d.terminal) || undefined,
          expiresAt: str(d.expires_at) || undefined,
          askedAt: e.time,
        };
        approvals.set(a.id, a);
        const step = a.callId ? calls.get(a.callId) : undefined;
        if (step) step.approval = a;
        else {
          const r = runOf(e);
          if (r) r.steps.push({ kind: 'approval', seq: e.seq, approval: a, at: e.time });
        }
        break;
      }
      case 'approval.resolved': {
        const a = approvals.get(str(d.approval_id));
        if (a)
          a.outcome = {
            allowed: d.allowed === true,
            by: str(d.by),
            message: str(d.message) || undefined,
            withdrawn: d.withdrawn === true,
            at: e.time,
          };
        break;
      }
      case 'usage': {
        const scope = str(d.scope);
        if (scope === 'account') {
          t.limits = d.limits as Limits;
          break;
        }
        const r = runOf(e);
        if (!r) break;
        if (scope === 'step') {
          if (str(d.model)) r.model = str(d.model);
          r.live.input += num(d.input_tokens);
          r.live.output += num(d.output_tokens);
          r.live.cacheRead += num(d.cache_read_tokens);
          r.live.cacheWrite += num(d.cache_write_tokens);
        } else if (scope === 'run') {
          const models = Array.isArray(d.models) ? d.models.filter((m) => typeof m === 'string' && m) : [];
          if (!r.model && models.length > 0) r.model = models.join(', ');
          r.usage = {
            input: num(d.input_tokens),
            output: num(d.output_tokens),
            cacheRead: num(d.cache_read_tokens),
            cacheWrite: num(d.cache_write_tokens),
            turns: num(d.turns),
            durationMs: typeof d.duration_ms === 'number' && d.duration_ms > 0 ? d.duration_ms : undefined,
            costUsd: typeof d.list_cost_usd === 'number' ? d.list_cost_usd : undefined,
          };
        }
        break;
      }
      case 'error': {
        const r = runOf(e);
        if (r) r.steps.push({ kind: 'error', seq: e.seq, message: str(d.message), at: e.time });
        break;
      }
      case 'status':
        t.status = { state: str(d.state), message: str(d.message) || undefined, at: e.time };
        break;
    }
  }

  t.pending = [...approvals.values()].filter((a) => !a.outcome);
  return t;
}
