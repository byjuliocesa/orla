import type { RunView, Step, ToolStep } from './timeline';
import type { Flow, FlowStep } from './types';

// Where a flow stands, read from what the AI does: the tool calls it makes (its own and its
// subagents') and the "▶ step" lines the flow asks it to write. Nothing is asked of the model,
// so following a flow live costs no tokens.

export interface Lane {
  /** "main", or the id of the Agent call that started a subagent. */
  id: string;
  label: string;
  /** The step the lane is at, the latest matched. */
  current?: string;
  visited: string[];
  /** Every step the lane went through, in order, a step repeated only when it came back to it. */
  path: string[];
  /** When the lane was in each step: from entering it to entering the next, in ms. */
  spans: { step: string; start: number; end?: number }[];
  /** The lane's work is over: its subagent returned, or the run ended. */
  over: boolean;
}

export interface FlowState {
  run: RunView;
  lanes: Lane[];
  /** The run is over and a lane reached the last step. */
  finished: boolean;
  /** When the run last did something, in ms. */
  lastAt: number;
  /** A question waits for the owner (a permission). */
  asking: boolean;
  /** Time spent in each step this run: what is done, and since when it is still going. */
  times: Record<string, StepTime>;
}

export interface StepTime {
  /** Time already spent, closed spans only. */
  ms: number;
  /** Set while the step is still going: since when. */
  since?: number;
}

/** A step's time at a moment: what is done, plus the part still going. */
export const elapsed = (t: StepTime, now: number) => t.ms + (t.since ? Math.max(0, now - t.since) : 0);

export type FlowStatus = 'live' | 'asking' | 'quiet' | 'done' | 'stopped' | 'failed';

/** Quiet this long while running, a flow looks stuck. */
export const QUIET_MS = 3 * 60_000;

/** How the flow's last run stands: following now, waiting, gone quiet, done, or ended short. */
export function flowStatus(state: FlowState | null, now: number): FlowStatus | null {
  if (!state) return null;
  if (state.run.status === 'running') {
    if (state.asking) return 'asking';
    return now - state.lastAt > QUIET_MS ? 'quiet' : 'live';
  }
  if (state.finished) return 'done';
  return state.run.status === 'failed' ? 'failed' : 'stopped';
}

/** The step the flow is at, or stopped at. */
export function stepAt(state: FlowState | null): string | undefined {
  return state?.lanes.find((l) => !l.over)?.current ?? state?.lanes[state.lanes.length - 1]?.current;
}

/** The moves made from one step to another, as "from>to". */
export function moves(state: FlowState | null): Set<string> {
  const out = new Set<string>();
  for (const l of state?.lanes ?? []) for (let i = 1; i < l.path.length; i++) out.add(`${l.path[i - 1]}>${l.path[i]}`);
  return out;
}

const slug = (s: string) =>
  s
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');

const glob = (pattern: string, text: string) => {
  const re = new RegExp('^' + pattern.split('*').map((p) => p.replace(/[.+?^${}()|[\]\\]/g, '\\$&')).join('.*') + '$', 'is');
  return re.test(text.trim());
};

const field = (input: unknown, key: string) => {
  const v = (input as Record<string, unknown> | null)?.[key];
  return typeof v === 'string' ? v : '';
};

/** What a signal like Bash(gh pr create*) is compared with in a call to that tool. */
const argument = (tool: ToolStep) => {
  if (tool.name === 'Bash') return field(tool.input, 'command');
  if (tool.name === 'Skill') return field(tool.input, 'skill') || field(tool.input, 'command');
  return JSON.stringify(tool.input ?? '');
};

function signalMatches(signal: string, tool: ToolStep) {
  const m = /^([^()]+)\((.*)\)$/s.exec(signal.trim());
  const name = (m?.[1] ?? signal).trim();
  if (!glob(name, tool.name)) return false;
  if (!m) return true;
  const arg = argument(tool);
  if (glob(m[2] ?? '', arg)) return true;
  // A chained command (cd into a worktree && git status) matches by any of its parts.
  return tool.name === 'Bash' && arg.split(/&&|\|\||;|\|/).some((part) => glob(m[2] ?? '', part));
}

function stepForCall(flow: Flow, tool: ToolStep): FlowStep | undefined {
  const skill = tool.name === 'Skill' ? argument(tool) : '';
  return flow.steps.find((s) => s.signals.some((sig) => signalMatches(sig, tool)) || (skill !== '' && s.uses.includes(skill)));
}

/** Every "▶ step" line of a message, in order: one message may go through several steps. */
function stepsForText(flow: Flow, text: string): FlowStep[] {
  const found: FlowStep[] = [];
  for (const line of text.split('\n')) {
    const m = /^\s*(?:[*_`]*)\s*▶\s*(.+?)\s*[*_`]*\s*$/.exec(line);
    if (!m) continue;
    const said = slug((m[1] ?? '').replace(/^\d+[.)]\s*/, ''));
    const step = flow.steps.find((s) => s.id === said || slug(s.title) === said) ?? flow.steps.find((s) => said.startsWith(slug(s.title)));
    if (step) found.push(step);
  }
  return found;
}

const isAgent = (t: ToolStep) => t.name === 'Agent' || t.name === 'Task';

/**
 * How far the latest run that touched the flow got. A lane is the main conversation or one
 * subagent; each moves through the steps on its own, so three tasks at once show as three.
 */
export function trackFlow(flow: Flow, runs: RunView[]): FlowState | null {
  for (let i = runs.length - 1; i >= 0; i--) {
    const state = trackRun(flow, runs[i]!);
    if (state) return state;
  }
  return null;
}

/**
 * How long each step usually takes: the median of the runs that followed the flow and ended,
 * the run in view left out. Steps never timed before are absent.
 */
export function usualTimes(flow: Flow, runs: RunView[], except?: RunView): Record<string, number> {
  const seen: Record<string, number[]> = {};
  for (const run of runs) {
    if (run === except || run.status === 'running') continue;
    const st = trackRun(flow, run);
    if (!st) continue;
    for (const [id, t] of Object.entries(st.times)) if (!t.since && t.ms > 0) (seen[id] ??= []).push(t.ms);
  }
  const out: Record<string, number> = {};
  for (const [id, list] of Object.entries(seen)) {
    list.sort((a, b) => a - b);
    out[id] = list[Math.floor(list.length / 2)]!;
  }
  return out;
}

/** Did the step go fast, as usual, slow, or very slow, against how long it usually takes. */
export function pace(ms: number, usual: number | undefined): 'fast' | 'usual' | 'slow' | 'stalled' | undefined {
  if (!usual || usual < 1000) return undefined;
  const r = ms / usual;
  return r < 0.6 ? 'fast' : r <= 1.5 ? 'usual' : r <= 3 ? 'slow' : 'stalled';
}

function trackRun(flow: Flow, run: RunView): FlowState | null {
  {
    const lanes = new Map<string, Lane>();
    // A run counts only when it followed the flow: called its skill, or said a "▶ step" of it.
    // A git status in some other run is not this flow being followed.
    let invoked = false;
    let marked = false;
    const hit = (laneId: string, label: string, step: FlowStep | undefined, at: string) => {
      if (!step) return;
      let lane = lanes.get(laneId);
      if (!lane) {
        lane = { id: laneId, label, visited: [], path: [], spans: [], over: false };
        lanes.set(laneId, lane);
      }
      const t = Date.parse(at) || lastAt;
      if (lane.current !== step.id) {
        const open = lane.spans[lane.spans.length - 1];
        if (open && open.end === undefined) open.end = Math.max(open.start, t);
        lane.spans.push({ step: step.id, start: t });
      }
      lane.current = step.id;
      if (!lane.visited.includes(step.id)) lane.visited.push(step.id);
      if (lane.path[lane.path.length - 1] !== step.id) lane.path.push(step.id);
    };
    let lastAt = Date.parse(run.started) || 0;
    let asking = false;
    const walk = (steps: Step[], laneId: string, label: string) => {
      for (const s of steps) {
        lastAt = Math.max(lastAt, Date.parse(s.at) || 0, s.kind === 'tool' && s.result ? Date.parse(s.result.at) || 0 : 0);
        if ((s.kind === 'approval' && !s.approval.outcome) || (s.kind === 'tool' && s.approval && !s.approval.outcome)) asking = true;
        if (s.kind === 'message') {
          for (const said of stepsForText(flow, s.text)) {
            marked = true;
            hit(laneId, label, said, s.at);
          }
        }
        if (s.kind !== 'tool') continue;
        if (s.name === 'Skill' && argument(s) === flow.name) invoked = true;
        hit(laneId, label, stepForCall(flow, s), s.at);
        if (isAgent(s) && s.children.length > 0) {
          walk(s.children, s.callId, field(s.input, 'description') || 'Subagent');
          const lane = lanes.get(s.callId);
          if (lane && s.result) {
            // The subagent is done: so is the step it was in, now, not when the whole run ends.
            lane.over = true;
            const open = lane.spans[lane.spans.length - 1];
            if (open && open.end === undefined) open.end = Math.max(open.start, Date.parse(s.result.at) || open.start);
          }
        }
      }
    };
    walk(run.steps, 'main', 'Main');
    if (!invoked && !marked) return null;
    const ended = run.status !== 'running';
    // With subagents, what the main conversation did before handing out the work was getting
    // ready, not a task: it is left out, and so is the main lane when nothing else is left.
    const firstAgent = Math.min(...run.steps.filter((x) => x.kind === 'tool' && isAgent(x)).map((x) => Date.parse(x.at) || Infinity));
    const main = lanes.get('main');
    if (main && lanes.size > 1 && Number.isFinite(firstAgent)) {
      const kept = main.spans.filter((sp) => sp.start >= firstAgent);
      if (kept.length === 0) lanes.delete('main');
      else {
        main.spans = kept;
        main.path = kept.map((sp) => sp.step).filter((id, i, all) => i === 0 || all[i - 1] !== id);
        main.visited = [...new Set(main.path)];
      }
    }
    const list = [...lanes.values()].map((l) => ({ ...l, over: l.over || ended }));
    // Subagents first: when several tasks run at once, the main conversation only hands them out.
    list.sort((a, b) => (a.id === 'main' ? 1 : 0) - (b.id === 'main' ? 1 : 0));
    const last = flow.steps[flow.steps.length - 1]?.id;
    // Done when a lane got to the end: the last step, or a step that leads to the end.
    const ends = new Set(flow.steps.filter((x, i) => x.next === 'end' || (!x.next && i === flow.steps.length - 1)).map((x) => x.id));
    if (last) ends.add(last);
    // A step's time runs from entering it to entering the next; the last one stops with the run.
    const stop = ended ? Date.parse(run.finished ?? '') || lastAt : undefined;
    // Each lane's time in a step; the step shows the longest, since tasks run side by side and a
    // sum would count the same seconds more than once.
    const times: Record<string, StepTime> = {};
    for (const l of list) {
      const mine: Record<string, StepTime> = {};
      for (const sp of l.spans) {
        const t = (mine[sp.step] ??= { ms: 0 });
        const end = sp.end ?? stop;
        if (end !== undefined) t.ms += Math.max(0, end - sp.start);
        else t.since = Math.min(t.since ?? sp.start, sp.start);
      }
      for (const [id, t] of Object.entries(mine)) {
        const cur = times[id];
        if (!cur || elapsed(t, Date.now()) > elapsed(cur, Date.now())) times[id] = t;
      }
    }
    return { run, lanes: list, finished: ended && run.status === 'done' && list.some((l) => !!l.current && ends.has(l.current)), lastAt, asking, times };
  }
}
