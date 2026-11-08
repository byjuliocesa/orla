import { describe, expect, it } from 'vitest';
import { elapsed, moves, pace, trackFlow, usualTimes } from './flowtrack';
import type { RunView, Step, ToolStep } from './timeline';
import type { Flow } from './types';

const flow: Flow = {
  name: 'fluxo-erp',
  title: 'ERP Bugfix',
  description: '',
  scope: 'user',
  dir: '/x',
  path: '/x/SKILL.md',
  files: [],
  modified: '',
  steps: [
    { id: 'board', title: 'Board', uses: [], signals: ['mcp__jira__*'], branches: [], body: '' },
    { id: 'alteracoes', title: 'Alterações', uses: [], signals: [], branches: [], body: '' },
    { id: 'pr', title: 'PR', uses: ['pr-padrao'], signals: ['Bash(gh pr create*)'], branches: [], body: '' },
  ],
};

let n = 0;
const tool = (name: string, input: unknown, children: Step[] = [], done = true): ToolStep => ({
  kind: 'tool',
  seq: ++n,
  callId: `c${n}`,
  name,
  input,
  at: '',
  children,
  result: done ? { ok: true, output: '', truncated: false, interrupted: false, at: '' } : undefined,
});
const say = (text: string): Step => ({ kind: 'message', seq: ++n, text, at: '' });
const run = (steps: Step[], status: RunView['status'] = 'running'): RunView => ({
  id: `r${n}`,
  prompt: '',
  interactive: false,
  started: '',
  status,
  steps,
  live: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
});

describe('trackFlow', () => {
  it('follows the main conversation by its calls and its ▶ lines', () => {
    const s = trackFlow(flow, [run([tool('mcp__jira__get_issue', {}), say('Pronto.\n▶ Alterações')])]);
    expect(s?.lanes).toHaveLength(1);
    expect(s?.lanes[0]?.current).toBe('alteracoes');
    expect(s?.lanes[0]?.visited).toEqual(['board', 'alteracoes']);
    expect(s?.finished).toBe(false);
  });

  it('keeps the way taken, a branch back included, and knows when the flow is done', () => {
    const s = trackFlow(flow, [run([say('▶ Board'), say('▶ PR'), say('▶ Alterações'), say('▶ PR')], 'done')]);
    expect(s?.lanes[0]?.path).toEqual(['board', 'pr', 'alteracoes', 'pr']);
    expect(moves(s).has('pr>alteracoes')).toBe(true);
    expect(s?.finished).toBe(true);
  });

  it('shows each subagent on its own, at its own step', () => {
    const go = tool('Skill', { skill: 'fluxo-erp' });
    const a = tool('Agent', { description: 'Task 101' }, [tool('Bash', { command: 'gh pr create --fill' })]);
    const b = tool('Agent', { description: 'Task 102' }, [tool('mcp__jira__get_issue', {})], false);
    const s = trackFlow(flow, [run([go, a, b])]);
    const lanes = Object.fromEntries((s?.lanes ?? []).map((l) => [l.label, l]));
    expect(lanes['Task 101']?.current).toBe('pr');
    expect(lanes['Task 101']?.over).toBe(true);
    expect(lanes['Task 102']?.current).toBe('board');
    expect(lanes['Task 102']?.over).toBe(false);
  });

  it('knows a step by a skill it uses, and skips runs that never touched the flow', () => {
    const touched = run([tool('Skill', { skill: 'fluxo-erp' }), tool('Skill', { skill: 'pr-padrao' })], 'done');
    const later = run([tool('Bash', { command: 'ls' })], 'done');
    const s = trackFlow(flow, [touched, later]);
    expect(s?.run).toBe(touched);
    expect(s?.lanes[0]?.current).toBe('pr');
    expect(s?.lanes[0]?.over).toBe(true);
  });

  it('takes every ▶ line of a message, in order', () => {
    const s = trackFlow(flow, [run([say('▶ Board'), say('Sem README.\n▶ PR\nfeito\n▶ Alterações')])]);
    expect(s?.lanes[0]?.path).toEqual(['board', 'pr', 'alteracoes']);
  });

  it('times each step, from entering it to entering the next, and knows what is usual', () => {
    const at = (s: number) => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();
    const timed = (text: string, s: number): Step => ({ kind: 'message', seq: ++n, text, at: at(s) });
    const past = { ...run([timed('▶ Board', 0), timed('▶ PR', 10)], 'done'), started: at(0), finished: at(40) };
    const now = { ...run([timed('▶ Board', 100), timed('▶ PR', 160)]), started: at(100) };
    const st = trackFlow(flow, [past, now])!;
    expect(st.times.board).toEqual({ ms: 60_000 });
    expect(st.times.pr?.since).toBe(Date.parse(at(160)));
    expect(elapsed(st.times.pr!, Date.parse(at(170)))).toBe(10_000);
    const usual = usualTimes(flow, [past, now], st.run);
    expect(usual).toEqual({ board: 10_000, pr: 30_000 });
    expect(pace(60_000, usual.board)).toBe('stalled');
    expect(pace(5_000, usual.pr)).toBe('fast');
    expect(pace(30_000, usual.pr)).toBe('usual');
    expect(pace(5_000, undefined)).toBeUndefined();
  });

  it('closes a subagent\'s step when it returns, times a step by its longest task, and leaves out a main conversation that only handed out the work', () => {
    const at = (s: number) => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();
    const t = (name: string, input: unknown, s: number, children: Step[] = [], doneAt?: number): ToolStep => ({
      kind: 'tool', seq: ++n, callId: `t${n}`, name, input, at: at(s), children,
      result: doneAt === undefined ? undefined : { ok: true, output: '', truncated: false, interrupted: false, at: at(doneAt) },
    });
    const a = t('Agent', { description: 'A' }, 10, [t('mcp__jira__get_issue', {}, 11, [], 12), t('Bash', { command: 'cd /w/a && gh pr create' }, 13, [], 14)], 20);
    const b = t('Agent', { description: 'B' }, 10, [t('mcp__jira__get_issue', {}, 11, [], 12), t('Bash', { command: 'gh pr create' }, 15, [], 16)], 40);
    const st = trackFlow(flow, [{ ...run([t('Skill', { skill: 'fluxo-erp' }, 0), t('mcp__jira__get_issue', {}, 1, [], 2), a, b]), started: at(0) }])!;
    expect(st.lanes.map((l) => l.label)).toEqual(['A', 'B']);
    // A: in PR from 13 to its return at 20 (7s); B: from 15 to 40 (25s). The step shows 25s.
    expect(st.times.pr).toEqual({ ms: 25_000 });
    expect(st.times.board).toEqual({ ms: 4_000 });
  });

  it('is nothing when no run followed it, even one that made a call a step looks for', () => {
    expect(trackFlow(flow, [run([say('hello')])])).toBeNull();
    expect(trackFlow(flow, [run([tool('mcp__jira__get_issue', {})], 'done')])).toBeNull();
  });
});
