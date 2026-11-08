import type { Flow } from './types';

// Where each step of a flow sits on the canvas. The main path (each step's next, from the first)
// runs down the first column; a step reached only by a branch goes in a column to the right,
// level with the step it branches from, so a conditional reads as a path of its own.

export interface Cell {
  col: number;
  row: number;
}

export interface Edge {
  from: string; // a step id, or "start"
  to: string; // a step id, or "end"
  kind: 'next' | 'branch';
  when?: string;
}

export interface Layout {
  cells: Map<string, Cell>;
  end: Cell;
  edges: Edge[];
  cols: number;
  rows: number;
}

export function nextOf(flow: Flow, i: number): string {
  const s = flow.steps[i]!;
  if (s.next === 'end') return 'end';
  if (s.next && flow.steps.some((x) => x.id === s.next)) return s.next;
  return flow.steps[i + 1]?.id ?? 'end';
}

export function layout(flow: Flow): Layout {
  const index = new Map(flow.steps.map((s, i) => [s.id, i]));
  const cells = new Map<string, Cell>();
  const edges: Edge[] = [];
  if (flow.steps.length === 0) return { cells, end: { col: 0, row: 0 }, edges, cols: 1, rows: 1 };

  // The main path.
  let id: string | undefined = flow.steps[0]!.id;
  let row = 0;
  while (id && id !== 'end' && !cells.has(id)) {
    cells.set(id, { col: 0, row: row++ });
    id = nextOf(flow, index.get(id)!);
  }
  const taken = new Set([...cells.values()].map((c) => `${c.col}:${c.row}`));
  const put = (sid: string, col: number, at: number) => {
    let r = Math.max(0, at);
    while (taken.has(`${col}:${r}`)) r++;
    cells.set(sid, { col, row: r });
    taken.add(`${col}:${r}`);
  };

  // What leads into each step, to place the rest next to where they are reached from.
  const into = new Map<string, { from: string; kind: 'next' | 'branch' }[]>();
  flow.steps.forEach((s, i) => {
    const n = nextOf(flow, i);
    if (n !== 'end') into.set(n, [...(into.get(n) ?? []), { from: s.id, kind: 'next' }]);
    for (const b of s.branches) if (index.has(b.to)) into.set(b.to, [...(into.get(b.to) ?? []), { from: s.id, kind: 'branch' }]);
  });
  for (let pass = 0; pass < flow.steps.length; pass++) {
    let moved = false;
    for (const s of flow.steps) {
      if (cells.has(s.id)) continue;
      const from = (into.get(s.id) ?? []).filter((e) => cells.has(e.from));
      if (from.length === 0) continue;
      // Below the step that leads to it on its own path, or beside the one that branches to it.
      const byNext = from.find((e) => e.kind === 'next' && cells.get(e.from)!.col > 0);
      if (byNext) {
        const c = cells.get(byNext.from)!;
        put(s.id, c.col, c.row + 1);
      } else {
        const c = cells.get(from[0]!.from)!;
        put(s.id, c.col + 1, c.row);
      }
      moved = true;
    }
    if (!moved) break;
  }
  // Steps nothing leads to still show, at the bottom of the side.
  let spare = Math.max(0, ...[...cells.values()].map((c) => c.row)) + 1;
  for (const s of flow.steps) if (!cells.has(s.id)) put(s.id, 1, spare++);

  edges.push({ from: 'start', to: flow.steps[0]!.id, kind: 'next' });
  flow.steps.forEach((s, i) => {
    edges.push({ from: s.id, to: nextOf(flow, i), kind: 'next' });
    for (const b of s.branches) if (index.has(b.to)) edges.push({ from: s.id, to: b.to, kind: 'branch', when: b.when });
  });

  const all = [...cells.values()];
  const mainRows = all.filter((c) => c.col === 0).length;
  const rows = Math.max(...all.map((c) => c.row)) + 1;
  return { cells, end: { col: 0, row: Math.max(mainRows, 1) }, edges, cols: Math.max(...all.map((c) => c.col)) + 1, rows: Math.max(rows, mainRows + 1) };
}
