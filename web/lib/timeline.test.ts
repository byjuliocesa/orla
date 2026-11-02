import { describe, expect, it } from 'vitest';
import { buildTimeline } from './timeline';
import type { OrlaEvent } from './types';

let seq = 0;
const ev = (type: string, data: Record<string, unknown>, run = 'run_1'): OrlaEvent => ({
  id: `evt_${++seq}`,
  seq,
  time: new Date(Date.UTC(2026, 10, 2, 12, 0, seq)).toISOString(),
  session: 'ses_1',
  run,
  type,
  data,
});

describe('buildTimeline', () => {
  it('pairs each call with its result, its approval and its subagent steps', () => {
    const t = buildTimeline([
      ev('run.started', { prompt: 'fix the login test', interactive: true, terminal: 'term_1' }),
      ev('tool.call', { call_id: 'c1', name: 'Task', input: { prompt: 'look around' } }),
      ev('tool.call', { call_id: 'c2', name: 'Read', input: { file_path: 'a.go' }, parent_call: 'c1' }),
      ev('tool.result', { call_id: 'c2', ok: true, output: 'package a' }),
      ev('tool.result', { call_id: 'c1', ok: true, output: 'done looking' }),
      ev('tool.call', { call_id: 'c3', name: 'Bash', input: { command: 'go test ./...' } }),
      ev('approval.requested', { approval_id: 'apr_1', tool: 'Bash', input: {}, call_id: 'c3', terminal: 'term_1' }),
      ev('approval.resolved', { approval_id: 'apr_1', allowed: false, by: 'web', message: 'not now' }),
      ev('tool.result', { call_id: 'c3', ok: false, output: 'not now' }),
      ev('usage', { scope: 'step', input_tokens: 3, output_tokens: 40 }),
      ev('usage', { scope: 'run', turns: 2, input_tokens: 4, output_tokens: 84, list_cost_usd: 0.01 }),
      ev('run.finished', { status: 'done' }),
    ]);

    expect(t.runs).toHaveLength(1);
    const r = t.runs[0]!;
    expect(r.prompt).toBe('fix the login test');
    expect(r.terminal).toBe('term_1');
    expect(r.status).toBe('done');
    expect(r.steps.map((s) => s.kind)).toEqual(['tool', 'tool']);
    const [task, bash] = r.steps as [any, any];
    expect(task.children).toHaveLength(1);
    expect(task.children[0].result.output).toBe('package a');
    expect(bash.approval.outcome).toMatchObject({ allowed: false, by: 'web', message: 'not now' });
    expect(bash.result.ok).toBe(false);
    expect(r.live.output).toBe(40);
    expect(r.usage).toMatchObject({ turns: 2, output: 84, costUsd: 0.01 });
    expect(t.pending).toEqual([]);
    expect(t.lastSeq).toBe(seq);
  });

  it('keeps an approval without a known call as its own step, pending until resolved', () => {
    const t = buildTimeline([
      ev('run.started', { prompt: 'deploy' }),
      ev('approval.requested', { approval_id: 'apr_2', tool: 'Bash', input: { command: 'make deploy' } }),
    ]);
    expect(t.runs[0]!.steps[0]!.kind).toBe('approval');
    expect(t.pending.map((a) => a.id)).toEqual(['apr_2']);
  });

  it('reports a withdrawn request as no longer pending', () => {
    const t = buildTimeline([
      ev('approval.requested', { approval_id: 'apr_3', tool: 'Edit', input: {} }, 'run_2'),
      ev('approval.resolved', { approval_id: 'apr_3', allowed: false, by: 'cancel', withdrawn: true }, 'run_2'),
    ]);
    expect(t.pending).toEqual([]);
    expect(t.runs[0]!.status).toBe('running');
  });
});
