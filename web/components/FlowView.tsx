import '@/styles/flow.css';
import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type WheelEvent } from 'react';
import { api } from '@/lib/api';
import { elapsed, flowStatus, moves, pace, stepAt, trackFlow, usualTimes, type FlowState, type FlowStatus, type Lane } from '@/lib/flowtrack';
import { layout } from '@/lib/flowlayout';
import { useNow } from '@/lib/hooks';
import type { RunView } from '@/lib/timeline';
import type { Flow, FlowStep, Session } from '@/lib/types';
import { Archive, AtSign, ChevronRight, Clock, Copy, Cross, Pencil, Plus, Trash } from './Icons';
import { commandInPrompt, insertIntoPrompt } from './PromptBox';
import { Rich } from './Run';
import { ask } from './ConfirmDialog';
import { toast } from './Toast';
import { Tooltip } from './Tooltip';

// The canvas: steps top to bottom, the trigger above them, branches drawn on the right.
const W = 300;
const H = 66;
const GAP = 46;
const TOP = 112;
const COL_GAP = 190;

const CACHE = (cwd: string) => `orla.flows.${cwd}`;
const PICK = (session: string) => `orla.flow.${session}`;

function cached(cwd: string): Flow[] | null {
  try {
    const v = localStorage.getItem(CACHE(cwd));
    return v ? (JSON.parse(v) as Flow[]) : null;
  } catch {
    return null;
  }
}

const NEW_MARK = (session: string) => `orla.newflow.${session}`;

/**
 * The flows this session follows, drawn as a canvas: what triggers each, its steps, the skills
 * each step uses, and where the AI is in it right now. The AI writes and rewrites the flows;
 * this page reads them again every few seconds, and sends steps to the chat to talk about.
 */
export function FlowView({ session, runs, onChanged }: { session: Session; runs: RunView[]; onChanged: () => void }) {
  const [library, setLibrary] = useState<Flow[] | null>(() => cached(session.cwd));
  const [error, setError] = useState('');
  const taken = session.flows ?? [];
  const [pick, setPickState] = useState<string>(() => {
    try {
      return localStorage.getItem(PICK(session.id)) ?? '';
    } catch {
      return '';
    }
  });
  const setPick = (name: string) => {
    setPickState(name);
    try {
      localStorage.setItem(PICK(session.id), name);
    } catch {}
  };

  // Bumped after the owner archives, restores or deletes from the buttons, to read again now.
  const [tick, setTick] = useState(0);
  // The AI edits flows while it works; the page follows by reading them again, cheaply.
  useEffect(() => {
    let stale = false;
    const load = () =>
      api.flows(session.cwd).then(
        (r) => {
          if (stale) return;
          setError('');
          setLibrary((cur) => (JSON.stringify(cur) === JSON.stringify(r.flows) ? cur : r.flows));
          try {
            localStorage.setItem(CACHE(session.cwd), JSON.stringify(r.flows));
          } catch {}
        },
        (err) => !stale && setError(err instanceof Error ? err.message : 'Orla did not read the flows.'),
      );
    void load();
    const t = window.setInterval(() => document.visibilityState === 'visible' && void load(), 4000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [session.cwd, tick]);

  const mine = useMemo(() => (library ?? []).filter((f) => taken.includes(f.name)), [library, taken]);
  const others = useMemo(() => (library ?? []).filter((f) => !taken.includes(f.name)), [library, taken]);
  const flow = mine.find((f) => f.name === pick) ?? mine[0];
  const [center, setCenter] = useState(0);

  const setFlows = async (names: string[], note: string) => {
    try {
      await api.updateSession(session.id, { flows: names });
      onChanged();
      toast(note, 'info');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'The change did not reach Orla.');
    }
  };
  const take = (f: Flow) => {
    setPick(f.name);
    void setFlows([...taken, f.name], `${f.title} is followed from the next message.`);
  };
  const drop = async (f: Flow) => {
    const ok = await ask({ title: `Stop following ${f.title} here?`, body: 'The flow stays in your library; only this session stops following it. Add it back with +.', confirm: 'Stop following', danger: false });
    if (ok) void setFlows(taken.filter((n) => n !== f.name), `${f.title} is no longer followed here.`);
  };
  // Archiving, restoring and deleting are the owner's own: a button, a yes, done.
  const act: Act = async (action, f, step) => {
    const what = step ? `the step ${step.title} of ${f.title}` : f.title;
    if (action === 'delete' && !(await ask({ title: `Delete ${step ? step.title : f.title}?`, body: `${what} is removed for good. Archive it instead to keep it.`, confirm: 'Delete', danger: true }))) return false;
    if (action === 'archive' && !(await ask({ title: `Archive ${step ? step.title : f.title}?`, body: step ? 'The flow goes on without it; it waits among the flow’s archived steps.' : 'No session follows it any more; it waits in the archive.', confirm: 'Archive', danger: false }))) return false;
    try {
      const r = await api.flowAction(action, f.name, step?.id, session.cwd);
      if (action === 'duplicate' && r.name) {
        // The copy is followed here at once, and shown, ready to be changed.
        setPick(r.name);
        await setFlows([...taken, r.name], `${f.title} (copy) is followed here.`);
      } else {
        toast(`${step ? step.title : f.title} ${action === 'delete' ? 'deleted' : action === 'archive' ? 'archived' : 'restored'}.`, 'info');
      }
      setTick((t) => t + 1);
      return true;
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not change the flow.');
      return false;
    }
  };
  // Flows are written by the AI; the page puts the command in the prompt, for the owner to send.
  const command = (text: string) => commandInPrompt(text);
  // /newflow asks for the flow in the chat; the flow it writes is taken here when it appears.
  const newFlow = () => {
    try {
      localStorage.setItem(NEW_MARK(session.id), String(Date.now()));
    } catch {}
    command('/newflow');
  };
  useEffect(() => {
    if (!library) return;
    let since = 0;
    try {
      since = Number(localStorage.getItem(NEW_MARK(session.id)) ?? 0);
    } catch {}
    // A click on New flow counts for half an hour.
    if (since && Date.now() - since > 30 * 60_000) since = 0;
    // /newflow typed by hand counts too, while it runs and for a few minutes after: a flow
    // changed long after it (a copy, an edit) was not made by it.
    const typed = [...runs].reverse().find((r) => r.prompt.trimStart().startsWith('/newflow'));
    const recent = typed && (typed.status === 'running' || Date.now() - (Date.parse(typed.finished ?? '') || 0) < 10 * 60_000);
    if (typed && recent) since = Math.max(since, Date.parse(typed.started) - 1000);
    if (!since) return;
    // A flow taken this way and later dropped by the owner is not taken again.
    try {
      since = Math.max(since, Number(localStorage.getItem(`${NEW_MARK(session.id)}.done`) ?? 0));
    } catch {}
    const fresh = library.filter((f) => !taken.includes(f.name) && Date.parse(f.modified) > since);
    if (fresh.length === 0) return;
    try {
      localStorage.removeItem(NEW_MARK(session.id));
      localStorage.setItem(`${NEW_MARK(session.id)}.done`, String(Date.now()));
    } catch {}
    setPick(fresh[0]!.name);
    void setFlows([...taken, ...fresh.map((f) => f.name)], `${fresh.map((f) => f.title).join(', ')} is followed here.`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [library, runs.length]);

  if (!library) {
    return (
      <div className="fl-empty-wrap">
        {error ? <p className="fl-note">{error}</p> : <div className="fl-skel" role="status" aria-label="Reading the flows" />}
      </div>
    );
  }

  if (!flow) {
    // Nothing followed yet: the drawing and New flow; the flows already written, and the
    // archived ones, wait in the corner.
    return (
      <div className="fl">
        <div className="fl-bar">
          <AddFlow others={others} onTake={take} onNew={newFlow} />
          <ArchivedMenu act={act} tick={tick} />
        </div>
        <FlowEmpty onNew={newFlow} />
      </div>
    );
  }

  return (
    <div className="fl">
      <div className="fl-bar">
        <div className="fl-picks" role="tablist" aria-label="Flows of this session">
          {mine.map((f) => (
            <span key={f.name} className="fl-pick" data-on={f.name === flow.name || undefined}>
              <button type="button" role="tab" aria-selected={f.name === flow.name} onClick={() => setPick(f.name)}>
                <FlowMark flow={f} runs={runs} />
                {f.title}
              </button>
              <Tooltip label="Stop following here" side="bottom">
                <button type="button" className="fl-pick-x" aria-label={`Stop following ${f.title}`} onClick={() => void drop(f)}>
                  <Cross />
                </button>
              </Tooltip>
            </span>
          ))}
        </div>
        <AddFlow others={others} onTake={take} onNew={newFlow} />
        <ArchivedMenu act={act} tick={tick} />
        <Tooltip label="Back to the start" side="bottom">
          <button type="button" className="icon-btn" aria-label="Back to the start" onClick={() => setCenter((c) => c + 1)}>
            <Center />
          </button>
        </Tooltip>
      </div>
      <FlowCanvas key={flow.name} flow={flow} runs={runs} cwd={session.cwd} command={command} act={act} tick={tick} center={center} />
    </div>
  );
}

function FlowEmpty({ onNew }: { onNew: () => void }) {
  return (
    <div className="fl-empty-wrap">
      <div className="fl-empty">
        <div className="fl-empty-art" aria-hidden="true">
          <span className="fl-empty-step" style={{ width: 132 }}>
            <i>1</i>
            <b />
          </span>
          <span className="fl-empty-link" />
          <span className="fl-empty-step" style={{ width: 156 }}>
            <i>2</i>
            <b />
          </span>
          <span className="fl-empty-link" />
          <span className="fl-empty-step" style={{ width: 118 }}>
            <i>3</i>
            <b />
          </span>
        </div>
        <h2>No flows yet</h2>
        <p>The steps the AI follows for a kind of task.</p>
        <button type="button" className="btn btn-primary" onClick={onNew}>
          <Plus />
          New flow
        </button>
      </div>
    </div>
  );
}

function AddFlow({ others, onTake, onNew }: { others: Flow[]; onTake: (f: Flow) => void; onNew: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    addEventListener('mousedown', close);
    addEventListener('keydown', esc);
    return () => {
      removeEventListener('mousedown', close);
      removeEventListener('keydown', esc);
    };
  }, [open]);
  return (
    <div className="fl-add" ref={ref}>
      <Tooltip label="Add a flow" side="bottom">
        <button type="button" className="icon-btn" aria-label="Add a flow" aria-expanded={open} onClick={() => setOpen(!open)}>
          <Plus />
        </button>
      </Tooltip>
      {open && (
        <div className="fl-menu" role="menu">
          {others.map((f) => (
            <button
              key={f.name}
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                onTake(f);
              }}
            >
              <span>{f.title}</span>
              <span className="fl-menu-sub">{f.description}</span>
            </button>
          ))}
          {others.length > 0 && <hr />}
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onNew();
            }}
          >
            <span>New flow…</span>
            <span className="fl-menu-sub">The AI writes it.</span>
          </button>

        </div>
      )}
    </div>
  );
}

type Pick = { kind: 'flow' } | { kind: 'step'; id: string };

type Act = (action: 'archive' | 'restore' | 'delete' | 'duplicate', flow: Flow | { name: string; title: string }, step?: { id: string; title: string }) => Promise<boolean>;

function FlowCanvas({ flow, runs, cwd, command, act, tick, center }: { flow: Flow; runs: RunView[]; cwd: string; command: (text: string) => void; act: Act; tick: number; center: number }) {
  const [selected, setSelected] = useState<Pick | null>(null);
  const [view, setView] = useState({ x: 0, y: 0, z: 1 });
  useEffect(() => setView({ x: 0, y: 0, z: 1 }), [center]);
  const pan = useRef<{ x: number; y: number; vx: number; vy: number; moved: boolean } | null>(null);
  const state: FlowState | null = useMemo(() => trackFlow(flow, runs), [flow, runs]);
  // While a step runs, its clock ticks every second.
  const clock = useNow(state?.run.status === 'running' ? 1000 : 20_000);
  const status = flowStatus(state, clock);
  const usual = useMemo(() => usualTimes(flow, runs, state?.run), [flow, runs, state?.run]);
  const at = stepAt(state);
  const went = useMemo(() => moves(state), [state]);
  // The move a lane is making right now: the line into the step it is at.
  const now = new Set((state?.lanes ?? []).filter((l) => !l.over && l.path.length > 0).map((l) => (l.path.length > 1 ? `${l.path[l.path.length - 2]}>${l.path[l.path.length - 1]}` : `start>${l.path[0]}`)));
  const edgeState = (from: string, to: string) =>
    now.has(`${from}>${to}`) && status === 'live'
      ? 'live'
      : went.has(`${from}>${to}`) || (from === 'start' && (state?.lanes ?? []).some((l) => l.path[0] === to)) || (to === 'end' && state?.finished && from === at)
        ? 'done'
        : undefined;
  const live = status === 'live';
  const lanesAt = (id: string) => (state?.lanes ?? []).filter((l) => l.current === id);
  const visited = (id: string) => (state?.lanes ?? []).some((l) => l.visited.includes(id));

  const lay = useMemo(() => layout(flow), [flow]);
  const X = (c: number) => c * (W + COL_GAP);
  const Y = (r: number) => TOP + r * (H + GAP);
  const endTop = Y(lay.end.row) - GAP + 34;
  const loops = lay.edges.filter((e) => e.to !== 'end' && e.from !== 'start' && lay.cells.get(e.from)?.col === lay.cells.get(e.to)?.col && lay.cells.get(e.to)!.row !== lay.cells.get(e.from)!.row + 1).length;
  const width = X(lay.cols - 1) + W + 60 + loops * 18 + 200;
  const height = Math.max(endTop + 60, Y(lay.rows) + 20);

  const down = (e: ReactPointerEvent) => {
    if ((e.target as HTMLElement).closest('button, .fl-legend')) return;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    pan.current = { x: e.clientX, y: e.clientY, vx: view.x, vy: view.y, moved: false };
  };
  const move = (e: ReactPointerEvent) => {
    const p = pan.current;
    if (!p) return;
    const dx = e.clientX - p.x;
    const dy = e.clientY - p.y;
    if (Math.abs(dx) + Math.abs(dy) > 3) p.moved = true;
    setView((v) => ({ ...v, x: p.vx + dx, y: p.vy + dy }));
  };
  const up = () => {
    // A click on the empty canvas lets go of what was picked, and the panel goes.
    if (pan.current && !pan.current.moved) setSelected(null);
    pan.current = null;
  };
  const wheel = (e: WheelEvent) => {
    if (e.ctrlKey || e.metaKey) {
      const z = Math.min(1.6, Math.max(0.5, view.z * (e.deltaY < 0 ? 1.08 : 1 / 1.08)));
      setView((v) => ({ ...v, z }));
    } else {
      setView((v) => ({ ...v, x: v.x - e.deltaX, y: v.y - e.deltaY }));
    }
  };

  // Several tasks at once: each subagent has a colour and a number, and draws its own line on
  // the way it went, beside the others where they shared it.
  const tracks = useMemo(
    () => [...(state?.lanes ?? [])].filter((l) => l.path.length > 0).sort((a, b) => (a.spans[0]?.start ?? 0) - (b.spans[0]?.start ?? 0)),
    [state],
  );
  const multi = tracks.length > 1;
  const [focus, setFocus] = useState<string | null>(null);
  const colorOf = (id: string) => LANE_COLORS[Math.max(0, tracks.findIndex((l) => l.id === id)) % LANE_COLORS.length]!;
  const numberOf = (id: string) => tracks.findIndex((l) => l.id === id) + 1;
  const laneMoved = (l: Lane, from: string, to: string) => {
    if (from === 'start') return l.path[0] === to;
    if (to === 'end') return !!state?.finished && l.over && l.current === from;
    for (let i = 1; i < l.path.length; i++) if (l.path[i - 1] === from && l.path[i] === to) return true;
    return false;
  };
  const laneNow = (l: Lane, from: string, to: string) =>
    status === 'live' && !l.over && (l.path.length > 1 ? l.path[l.path.length - 2] === from && l.path[l.path.length - 1] === to : from === 'start' && l.path[0] === to);

  // Each line, drawn from where it leaves to where it arrives.
  let loop = 0;
  const lines = lay.edges.map((e, n) => {
    const from = lay.cells.get(e.from);
    const st = multi ? undefined : edgeState(e.from, e.to);
    let d = '';
    let label: { x: number; y: number; anchor: 'start' | 'middle' | 'end' } | null = null;
    if (e.from === 'start') {
      d = `M ${W / 2} 72 L ${W / 2} ${Y(0) - 3}`;
    } else if (!from) {
      return null;
    } else if (e.to === 'end') {
      const sx = X(from.col) + W / 2;
      const sy = Y(from.row) + H;
      const em = endTop + 15;
      if (from.col === 0 && from.row === lay.end.row - 1) {
        d = `M ${sx} ${sy} L ${W / 2} ${endTop - 3}`;
      } else {
        // Down the gap beside the nodes, never through them, then into the end from its side.
        const gx = from.col > 0 ? X(from.col) - 36 : X(0) - 36;
        const ex = from.col > 0 ? W / 2 + 49 : W / 2 - 49;
        d = `M ${sx} ${sy} C ${sx} ${sy + 24}, ${gx} ${sy + 4}, ${gx} ${sy + 34} L ${gx} ${em - 18} Q ${gx} ${em}, ${gx + (ex > gx ? 18 : -18)} ${em} L ${ex} ${em}`;
      }
    } else {
      const to = lay.cells.get(e.to);
      if (!to) return null;
      const ya = Y(from.row) + H / 2;
      const yb = Y(to.row) + H / 2;
      if (from.col === to.col && to.row === from.row + 1) {
        d = `M ${X(from.col) + W / 2} ${Y(from.row) + H} L ${X(to.col) + W / 2} ${Y(to.row) - 3}`;
      } else if (from.col === to.col) {
        const out = X(from.col) + W + 26 + loop++ * 18;
        const yb2 = yb + (to.row < from.row ? 8 : -8);
        d = `M ${X(from.col) + W} ${ya} C ${out} ${ya}, ${out} ${yb2}, ${X(to.col) + W + 3} ${yb2}`;
        label = { x: out + 6, y: (ya + yb2) / 2, anchor: 'start' };
      } else if (to.col > from.col) {
        // Out to a conditional path: above the middle, so a way back can run below it.
        const x1 = X(from.col) + W;
        const x2 = X(to.col);
        const y1 = ya - 12;
        const y2 = yb - 12;
        d = `M ${x1} ${y1} C ${x1 + COL_GAP / 2} ${y1}, ${x2 - COL_GAP / 2} ${y2}, ${x2 - 3} ${y2}`;
        // Over the arrow's end: two branches from one step reach different steps, so their
        // conditions never sit on each other.
        label = { x: x2 - 10, y: y2 - 11, anchor: 'end' };
      } else {
        // Back from a conditional path: from its foot to the step it returns to.
        const x1 = X(from.col);
        const x2 = X(to.col) + W;
        const y1 = ya + 12;
        const y2 = yb + 12;
        d = `M ${x1} ${y1} C ${x1 - COL_GAP / 2} ${y1}, ${x2 + COL_GAP / 2} ${y2}, ${x2 + 3} ${y2}`;
        label = { x: (x1 + x2) / 2, y: (y1 + y2) / 2 + 12, anchor: 'middle' };
      }
    }
    const branch = e.kind === 'branch';
    const strands = multi ? tracks.filter((l) => laneMoved(l, e.from, e.to)) : [];
    const sideways = !!from && e.to !== 'end' && lay.cells.get(e.to)?.col !== from.col;
    return (
      <g key={n} className={branch ? 'fl-branch-g' : undefined} data-state={st ?? (strands.length > 0 ? 'done' : undefined)}>
        <path className={branch ? 'fl-edge fl-branch' : 'fl-edge'} data-state={st} data-muted={strands.length > 0 || undefined} d={d} markerEnd={st ? 'url(#fl-arrow-on)' : 'url(#fl-arrow)'} />
        {strands.map((l) => {
          const k = tracks.indexOf(l);
          // Side by side, but never wider than the node's middle: many tasks pack closer.
          const gap = tracks.length > 1 ? Math.min(4, 28 / (tracks.length - 1)) : 0;
          const off = (k - (tracks.length - 1) / 2) * gap;
          return (
            <path
              key={l.id}
              className="fl-strand"
              d={d}
              stroke={colorOf(l.id)}
              transform={sideways ? `translate(0 ${off})` : `translate(${off} 0)`}
              data-live={laneNow(l, e.from, e.to) || undefined}
              data-dim={(focus !== null && focus !== l.id) || undefined}
            />
          );
        })}
        {branch && e.when && label && (
          <text className="fl-branch-label" x={label.x} y={label.y} textAnchor={label.anchor} dominantBaseline="middle">
            <title>{e.when}</title>
            {st ? '↩ ' : ''}
            {e.when.length > (label.anchor === 'start' ? 34 : 26) ? e.when.slice(0, label.anchor === 'start' ? 33 : 25) + '…' : e.when}
          </text>
        )}
      </g>
    );
  });

  const step = selected?.kind === 'step' ? flow.steps.find((s) => s.id === selected.id) : undefined;
  const stuck = status === 'stopped' || status === 'quiet' || status === 'asking' ? 'stopped' : status === 'failed' ? 'failed' : null;

  return (
    <div className="fl-body" data-panel={selected ? '' : undefined}>
      <div
        className="fl-canvas"
        // Focus on a node would scroll the canvas under the pan; the pan alone moves it.
        onScroll={(e) => {
          e.currentTarget.scrollTop = 0;
          e.currentTarget.scrollLeft = 0;
        }}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
        onWheel={wheel}
      >
        <div className="fl-plane" style={{ width, height, ['--plane-w' as string]: `${X(lay.cols - 1) + W}px`, transform: `translate(${view.x}px, ${view.y}px) scale(${view.z})` }}>
          <svg className="fl-edges" width={width} height={height} aria-hidden="true">
            <defs>
              <marker id="fl-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M1 1 7 4 1 7" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
              </marker>
              <marker id="fl-arrow-on" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M1 1 7 4 1 7" fill="none" stroke="#6aa1ff" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
              </marker>
            </defs>
            {lines}
          </svg>

          <button type="button" className="fl-trigger" aria-pressed={selected?.kind === 'flow'} style={{ width: W }} onClick={() => setSelected({ kind: 'flow' })}>
            <span className="fl-trigger-k">When</span>
            <span className="fl-trigger-v">{flow.description || 'No trigger yet'}</span>
          </button>

          {flow.steps.map((s, i) => {
            const cell = lay.cells.get(s.id)!;
            // Once the flow finished, every step it went through is done, the last one too.
            const here = state?.finished ? [] : lanesAt(s.id);
            const done = visited(s.id) && here.length === 0;
            const nodeState = here.length > 0 ? (s.id === at && stuck ? stuck : live && here.some((l) => !l.over) ? 'live' : 'last') : done ? 'done' : undefined;
            return (
              <button
                key={s.id}
                type="button"
                className="fl-node"
                data-state={nodeState}
                data-side={cell.col > 0 || undefined}
                aria-pressed={selected?.kind === 'step' && selected.id === s.id}
                style={{ top: Y(cell.row), left: X(cell.col), width: W, height: H }}
                onClick={() => setSelected({ kind: 'step', id: s.id })}
              >
                <span className="fl-num num">{done ? <Tick /> : nodeState === 'failed' ? '!' : i + 1}</span>
                {state?.times[s.id] && <StepClock ms={elapsed(state.times[s.id]!, clock)} usual={usual[s.id]} running={!!state.times[s.id]!.since && status === 'live'} />}
                <span className="fl-node-text">
                  <span className="fl-node-title">{s.title}</span>
                  <span className="fl-node-meta">
                    {s.uses.length > 0 ? s.uses.join(' · ') : s.body ? s.body.split('\n')[0]!.replace(/[#*_`>]/g, '').slice(0, 60) : 'No instructions yet'}
                  </span>
                </span>
                {multi && (
                  <span className="fl-visits">
                    {tracks
                      .filter((l) => l.visited.includes(s.id) && !here.includes(l))
                      .map((l) => (
                        <i key={l.id} style={{ background: colorOf(l.id) }} data-dim={(focus !== null && focus !== l.id) || undefined} />
                      ))}
                  </span>
                )}
                {here.length > 0 && <Lanes lanes={here} live={live} colorOf={multi ? colorOf : undefined} numberOf={numberOf} focus={focus} />}
              </button>
            );
          })}
          {flow.steps.length > 0 && (
            <span className="fl-end" data-state={state?.finished ? 'done' : undefined} style={{ top: endTop, left: W / 2 - 46 }}>
              {state?.finished ? <Tick /> : null}
              End
            </span>
          )}
        </div>
        {multi && (
          <div className="fl-legend" role="list" aria-label="Tasks running this flow">
            {tracks.map((l) => {
              const where = flow.steps.find((x) => x.id === l.current)?.title;
              return (
                <button
                  key={l.id}
                  type="button"
                  role="listitem"
                  className="fl-legend-item"
                  aria-pressed={focus === l.id}
                  data-dim={(focus !== null && focus !== l.id) || undefined}
                  onClick={() => setFocus(focus === l.id ? null : l.id)}
                >
                  <span className="fl-legend-num num" style={{ background: colorOf(l.id) }}>
                    {numberOf(l.id)}
                  </span>
                  <span className="fl-legend-name">{l.id === 'main' ? 'Main conversation' : l.label}</span>
                  <span className="fl-legend-at">
                    {!l.over && status === 'live' ? <span className="wave" /> : <Tick />}
                    {where}
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {selected && (
        <aside className="fl-panel" aria-label={step ? step.title : flow.title}>
          {step ? (
            <StepPanel
              flow={flow}
              step={step}
              index={flow.steps.indexOf(step)}
              cwd={cwd}
              command={command}
              act={act}
              time={state?.times[step.id] ? { ms: elapsed(state.times[step.id]!, clock), running: !!state.times[step.id]!.since && status === 'live' } : undefined}
              usual={usual[step.id]}
              onGone={() => setSelected(null)}
              onBack={() => setSelected({ kind: 'flow' })}
            />
          ) : (
            <FlowPanel flow={flow} command={command} act={act} tick={tick} onGone={() => setSelected(null)} />
          )}
        </aside>
      )}
    </div>
  );
}

const LANE_COLORS = ['#6aa1ff', '#f5a524', '#46c37b', '#c084fc', '#f472b6', '#2dd4bf'];

function Lanes({ lanes, live, colorOf, numberOf, focus }: { lanes: Lane[]; live: boolean; colorOf?: (id: string) => string; numberOf: (id: string) => number; focus: string | null }) {
  return (
    <span className="fl-lanes">
      {lanes.slice(0, 4).map((l) => (
        <Tooltip key={l.id} label={l.id === 'main' ? 'The main conversation is here' : `${l.label} is here`} side="top">
          <span
            className="fl-lane"
            data-live={(live && !l.over) || undefined}
            data-dim={(focus !== null && focus !== l.id) || undefined}
            style={colorOf ? { background: colorOf(l.id), color: '#0b1418' } : undefined}
          >
            {colorOf ? numberOf(l.id) : ''}
          </span>
        </Tooltip>
      ))}
      {lanes.length > 4 && <span className="fl-lane-more num">+{lanes.length - 4}</span>}
    </span>
  );
}

const reference = (flow: Flow, step?: FlowStep) => `@flow:${flow.name}${step ? `#${step.id}` : ''}`;

/** What can be done to a flow or one of its steps. Editing is the AI's, from a command in the
 * prompt; archiving and deleting are the owner's, from the button. */
function Actions({ flow, step, command, act, onGone }: { flow: Flow; step?: FlowStep; command: (text: string) => void; act: Act; onGone: () => void }) {
  const target = step ? `${flow.name} ${step.id}` : flow.name;
  const run = async (action: 'archive' | 'delete') => {
    if (await act(action, flow, step)) onGone();
  };
  return (
    <div className="fl-actions">
      <button type="button" className="fl-act" onClick={() => insertIntoPrompt(reference(flow, step))}>
        <AtSign />
        Reference
      </button>
      <button type="button" className="fl-act" onClick={() => command(`/editflow ${target}`)}>
        <Pencil />
        Edit
      </button>
      {!step && (
        <button type="button" className="fl-act" onClick={() => void act('duplicate', flow).then((ok) => ok && onGone())}>
          <Copy />
          Duplicate
        </button>
      )}
      <button type="button" className="fl-act fl-act-archive" onClick={() => void run('archive')}>
        <Archive />
        Archive
      </button>
      <button type="button" className="fl-act fl-act-delete" onClick={() => void run('delete')}>
        <Trash />
        Delete
      </button>
    </div>
  );
}

function FlowPanel({ flow, command, act, tick, onGone }: { flow: Flow; command: (text: string) => void; act: Act; tick: number; onGone: () => void }) {
  return (
    <div className="fl-panel-in">
      <div className="fl-panel-head">
        <h3>{flow.title}</h3>
        <span className="fl-scope">{flow.scope === 'project' ? 'This project' : 'Yours'}</span>
      </div>
      <section>
        <h4>When it is followed</h4>
        <p className="fl-text">{flow.description || 'No trigger yet.'}</p>
      </section>
      {flow.intro && (
        <section>
          <h4>Rules for the whole flow</h4>
          <div className="md fl-md">
            <Rich text={flow.intro} />
          </div>
        </section>
      )}
      <ArchivedSteps flow={flow} act={act} tick={tick} />
      <Actions flow={flow} command={command} act={act} onGone={onGone} />
    </div>
  );
}

/** The steps archived from this flow, to read and bring back. */
function ArchivedSteps({ flow, act, tick }: { flow: Flow; act: Act; tick: number }) {
  const [steps, setSteps] = useState<FlowStep[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  useEffect(() => {
    let stale = false;
    api.archivedFlows().then(
      (r) => !stale && setSteps(r.steps.filter((a) => a.flow === flow.name).map((a) => a.step)),
      () => {},
    );
    return () => {
      stale = true;
    };
  }, [flow.name, tick]);
  if (steps.length === 0) return null;
  return (
    <section>
      <h4>Archived steps</h4>
      <div className="fl-arch-list">
        {steps.map((s) => (
          <div key={s.id} className="fl-arch" data-open={open === s.id || undefined}>
            <div className="fl-arch-row">
              <button type="button" className="fl-arch-name" aria-expanded={open === s.id} onClick={() => setOpen(open === s.id ? null : s.id)}>
                <ChevronRight />
                <span>{s.title}</span>
              </button>
              <button type="button" className="fl-restore" onClick={() => void act('restore', flow, s)}>
                Restore
              </button>
            </div>
            {open === s.id && <div className="md fl-md fl-arch-body">{s.body ? <Rich text={s.body} /> : <p>No instructions.</p>}</div>}
          </div>
        ))}
      </div>
    </section>
  );
}

function StepPanel({
  flow,
  step,
  index,
  cwd,
  command,
  act,
  time,
  usual,
  onGone,
  onBack,
}: {
  flow: Flow;
  step: FlowStep;
  index: number;
  cwd: string;
  command: (text: string) => void;
  act: Act;
  time?: { ms: number; running: boolean };
  usual?: number;
  onGone: () => void;
  onBack: () => void;
}) {
  const titleOf = (id: string) => flow.steps.find((s) => s.id === id)?.title ?? id;
  return (
    <div className="fl-panel-in">
      <div className="fl-panel-head">
        <button type="button" className="fl-back" onClick={onBack} aria-label={`Back to ${flow.title}`}>
          <ChevronRight />
          {flow.title}
        </button>
        <h3>
          <span className="fl-panel-num num">{index + 1}</span>
          {step.title}
        </h3>
        {(time || usual) && (
          <p className="fl-took" data-pace={time ? pace(time.ms, usual) : undefined}>
            <Clock />
            {time ? `${time.running ? 'Running for' : 'Took'} ${duration(time.ms)}` : 'Not run in the last run'}
            {usual ? ` · usually ${duration(usual)}` : ''}
          </p>
        )}
      </div>
      {step.body ? (
        <div className="md fl-md">
          <Rich text={step.body} />
        </div>
      ) : (
        <p className="fl-text dim">No instructions yet.</p>
      )}
      {step.uses.length > 0 && (
        <section>
          <h4>Skills it uses</h4>
          {step.uses.map((u) => (
            <UsedSkill key={u} name={u} cwd={cwd} />
          ))}
        </section>
      )}
      {step.branches.length > 0 && (
        <section>
          <h4>Goes elsewhere</h4>
          <ul className="fl-branches">
            {step.branches.map((b, i) => (
              <li key={i}>
                to <b>{titleOf(b.to)}</b>
                {b.when && <> {b.when}</>}
              </li>
            ))}
          </ul>
        </section>
      )}
      {step.signals.length > 0 && (
        <section>
          <h4>Seen when</h4>
          <ul className="fl-files">
            {step.signals.map((s) => (
              <li key={s} className="mono">
                {s}
              </li>
            ))}
          </ul>
        </section>
      )}
      {step.file && (
        <section>
          <h4 className="mono">{step.file}</h4>
          {step.file_body ? (
            <div className="md fl-md">
              <Rich text={step.file_body} />
            </div>
          ) : (
            <p className="fl-text dim">The file is not there yet.</p>
          )}
        </section>
      )}
      <Actions flow={flow} step={step} command={command} act={act} onGone={onGone} />
    </div>
  );
}

function UsedSkill({ name, cwd }: { name: string; cwd: string }) {
  const [open, setOpen] = useState(false);
  const [doc, setDoc] = useState<string | null>(null);
  const [missing, setMissing] = useState('');
  useEffect(() => {
    if (!open || doc !== null || missing) return;
    api.skillDoc(name, cwd).then(
      (r) => setDoc(r.body.replace(/^---\n[\s\S]*?\n---\n?/, '')),
      (err) => setMissing(err instanceof Error ? err.message : 'Not found.'),
    );
  }, [open, doc, missing, name, cwd]);
  return (
    <div className="fl-skill" data-open={open || undefined}>
      <button type="button" className="fl-skill-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <ChevronRight />
        <span className="mono">{name}</span>
      </button>
      {open && (
        <div className="fl-skill-body">
          {missing ? (
            <p className="fl-text dim">{missing}</p>
          ) : doc === null ? (
            <p className="fl-text dim">Reading…</p>
          ) : (
            <div className="md fl-md">
              <Rich text={doc} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

const Tick = () => (
  <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M3.5 8.5 6.5 11.5 12.5 4.5" />
  </svg>
);
const Center = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
    <path d="M2.5 5.5v-3h3M10.5 2.5h3v3M13.5 10.5v3h-3M5.5 13.5h-3v-3" />
    <circle cx="8" cy="8" r="1.6" />
  </svg>
);

/** The archived flows, to look through and bring back. Steps archived from a flow are kept in
 * that flow's panel. */
function ArchivedMenu({ act, tick }: { act: Act; tick: number }) {
  const [open, setOpen] = useState(false);
  const [flows, setFlows] = useState<Flow[] | null>(null);
  const [unfolded, setUnfolded] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    let stale = false;
    api.archivedFlows().then(
      (r) => !stale && setFlows(r.flows),
      () => !stale && setFlows([]),
    );
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    addEventListener('mousedown', close);
    addEventListener('keydown', esc);
    return () => {
      stale = true;
      removeEventListener('mousedown', close);
      removeEventListener('keydown', esc);
    };
  }, [open, tick]);
  return (
    <div className="fl-add" ref={ref}>
      <Tooltip label="Archived flows" side="bottom">
        <button type="button" className="icon-btn" aria-label="Archived flows" aria-expanded={open} onClick={() => setOpen(!open)}>
          <Archive />
        </button>
      </Tooltip>
      {open && (
        <div className="fl-menu fl-archived">
          <h5>Archived flows</h5>
          {!flows ? (
            <p className="fl-arch-empty">Reading…</p>
          ) : flows.length === 0 ? (
            <p className="fl-arch-empty">No archived flows</p>
          ) : (
            flows.map((f) => (
              <div key={f.name} className="fl-arch" data-open={unfolded === f.name || undefined}>
                <div className="fl-arch-row">
                  <button type="button" className="fl-arch-name" aria-expanded={unfolded === f.name} onClick={() => setUnfolded(unfolded === f.name ? null : f.name)}>
                    <ChevronRight />
                    <span>{f.title}</span>
                    <span className="fl-arch-sub num">{f.steps.length} steps</span>
                  </button>
                  <button type="button" className="fl-restore" onClick={() => void act('restore', f)}>
                    Restore
                  </button>
                </div>
                {unfolded === f.name && (
                  <ol className="fl-arch-steps">
                    {f.steps.map((s) => (
                      <li key={s.id}>{s.title}</li>
                    ))}
                  </ol>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

const WORDS: Record<FlowStatus, string> = {
  live: 'Running',
  asking: 'Waiting for you',
  quiet: 'No news for a while',
  done: 'Finished',
  stopped: 'Stopped',
  failed: 'Failed',
};

/** Beside each flow's name, how its last run stands: rolling while it runs, a green tick when
 * it finished, yellow when it stopped short or went quiet, red when it failed. */
function FlowMark({ flow, runs }: { flow: Flow; runs: RunView[] }) {
  const state = useMemo(() => trackFlow(flow, runs), [flow, runs]);
  const status = flowStatus(state, useNow(20_000));
  if (!status) return null;
  const title = flow.steps.find((s) => s.id === stepAt(state))?.title;
  const label = status === 'done' ? WORDS.done : `${WORDS[status]}${title ? ` · ${title}` : ''}`;
  return (
    <Tooltip label={label} side="bottom">
      <span className="fl-mark" data-status={status} aria-label={label}>
        {status === 'live' ? <span className="wave" /> : status === 'done' ? <Tick /> : null}
      </span>
    </Tooltip>
  );
}

/** A span of time, short: 42s, 3m 05s, 1h 04m. */
export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
  return `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m`;
}

const PACE: Record<string, string> = { fast: 'Faster than usual', usual: 'As usual', slow: 'Slower than usual', stalled: 'Much slower than usual' };

/** How long a step took, or has been going: coloured against how long it usually takes. */
function StepClock({ ms, usual, running }: { ms: number; usual?: number; running: boolean }) {
  const p = pace(ms, usual);
  const label = usual ? `${PACE[p ?? 'usual'] ?? ''} · usually ${duration(usual)}` : running ? 'Running' : 'Time it took';
  return (
    <Tooltip label={label} side="top">
      <span className="fl-time num" data-pace={p} data-running={running || undefined}>
        <Clock />
        {duration(ms)}
      </span>
    </Tooltip>
  );
}

/** Is any flow of the session being followed right now: for the Flow tab's wave. */
export function useFlowRunning(session: Session, runs: RunView[]): boolean {
  const taken = session.flows ?? [];
  const key = taken.join(',');
  const running = runs[runs.length - 1]?.status === 'running';
  const [flows, setFlows] = useState<Flow[] | null>(() => cached(session.cwd));
  useEffect(() => {
    if (!key || !running) return;
    let stale = false;
    api.flows(session.cwd).then((r) => !stale && setFlows(r.flows), () => {});
    return () => {
      stale = true;
    };
  }, [session.cwd, key, running]);
  return useMemo(
    () => running && (flows ?? []).some((f) => taken.includes(f.name) && trackFlow(f, runs)?.run.status === 'running'),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [flows, runs, key, running],
  );
}

/**
 * In the right panel's Running: each flow being followed now, where it is, and on a click all
 * its steps with how long each took.
 */
export function RunningFlows({ session, runs }: { session: Session; runs: RunView[] }) {
  const taken = session.flows ?? [];
  const [flows, setFlows] = useState<Flow[] | null>(() => cached(session.cwd));
  const key = taken.join(',');
  const running = runs[runs.length - 1]?.status === 'running';
  useEffect(() => {
    if (!key || !running) return;
    let stale = false;
    const load = () => api.flows(session.cwd).then((r) => !stale && setFlows(r.flows), () => {});
    void load();
    const t = window.setInterval(load, 10_000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [session.cwd, key, running]);
  const clock = useNow(running ? 1000 : 60_000);
  const live = useMemo(
    () =>
      (flows ?? [])
        .filter((f) => taken.includes(f.name))
        .map((f) => ({ f, st: trackFlow(f, runs) }))
        .filter((x) => x.st?.run.status === 'running'),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [flows, runs, key],
  );
  return (
    <>
      {live.map(({ f, st }) => (
        <RunningFlow key={f.name} flow={f} state={st!} now={clock} />
      ))}
    </>
  );
}

function RunningFlow({ flow, state, now }: { flow: Flow; state: FlowState; now: number }) {
  const [open, setOpen] = useState(false);
  const status = flowStatus(state, now);
  const at = stepAt(state);
  const current = flow.steps.find((s) => s.id === at);
  const visited = new Set(state.lanes.flatMap((l) => l.visited));
  return (
    <div className="rf" data-open={open || undefined}>
      <button type="button" className="ss-task rf-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        {status === 'live' ? <span className="wave" /> : <span className="rf-dot" data-status={status} />}
        <span className="ss-task-name rf-name">
          <span>{flow.title}</span>
          <span className="rf-at">{status === 'asking' ? 'Waiting for you' : status === 'quiet' ? `No news · ${current?.title ?? ''}` : (current?.title ?? 'Starting')}</span>
        </span>
        <ChevronRight />
      </button>
      {open && (
        <ol className="rf-steps">
          {flow.steps.map((s, i) => {
            const t = state.times[s.id];
            const here = s.id === at;
            const done = visited.has(s.id) && !here;
            return (
              <li key={s.id} data-state={here ? 'here' : done ? 'done' : undefined}>
                <span className="rf-mark">{here ? <span className="wave" /> : done ? <Tick /> : <span className="num">{i + 1}</span>}</span>
                <span className="rf-title">{s.title}</span>
                {t && <span className="rf-time num">{duration(elapsed(t, now))}</span>}
              </li>
            );
          })}
        </ol>
      )}
    </div>
  );
}
