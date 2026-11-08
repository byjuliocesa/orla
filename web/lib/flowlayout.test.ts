import { describe, expect, it } from 'vitest';
import { layout } from './flowlayout';
import type { Flow, FlowStep } from './types';

const step = (id: string, more: Partial<FlowStep> = {}): FlowStep => ({ id, title: id, uses: [], signals: [], branches: [], body: '', ...more });
const flow = (steps: FlowStep[]): Flow => ({ name: 'f', title: 'F', description: '', scope: 'user', dir: '', path: '', files: [], modified: '', steps });

describe('layout', () => {
  it('puts a plain flow down one column', () => {
    const l = layout(flow([step('a'), step('b'), step('c')]));
    expect([...l.cells.values()]).toEqual([
      { col: 0, row: 0 },
      { col: 0, row: 1 },
      { col: 0, row: 2 },
    ]);
    expect(l.end).toEqual({ col: 0, row: 3 });
  });

  it('sets a conditional path beside the step it branches from', () => {
    const l = layout(
      flow([
        step('check', { branches: [{ to: 'log', when: 'if git' }] }),
        step('sizes'),
        step('wrap', { next: 'end' }),
        step('log', { next: 'sizes' }),
        step('readme', { next: 'end' }),
      ]),
    );
    expect(l.cells.get('check')).toEqual({ col: 0, row: 0 });
    expect(l.cells.get('sizes')).toEqual({ col: 0, row: 1 });
    expect(l.cells.get('wrap')).toEqual({ col: 0, row: 2 });
    expect(l.cells.get('log')).toEqual({ col: 1, row: 0 });
    // nothing leads to it: still shown, aside
    expect(l.cells.get('readme')?.col).toBe(1);
    expect(l.edges).toContainEqual({ from: 'log', to: 'sizes', kind: 'next' });
    expect(l.edges).toContainEqual({ from: 'wrap', to: 'end', kind: 'next' });
  });

  it('keeps the steps of a side path one under another', () => {
    const l = layout(flow([step('a', { branches: [{ to: 'x' }] }), step('b', { next: 'end' }), step('x'), step('y', { next: 'end' })]));
    expect(l.cells.get('x')).toEqual({ col: 1, row: 0 });
    expect(l.cells.get('y')).toEqual({ col: 1, row: 1 });
  });
});
