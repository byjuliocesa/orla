import '@/styles/automations.css';
import { useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { sessionName } from '@/lib/format';
import { useNow } from '@/lib/hooks';
import type { Automation, AutomationLine, AutomationStatus, Session } from '@/lib/types';
import { ask } from './ConfirmDialog';
import { ChevronRight, Pencil, Plus, Trash } from './Icons';
import { commandInPrompt } from './PromptBox';
import { toast } from './Toast';

/**
 * Automations: scripts Orla runs and watches. In a session, those that run with it and those
 * that run everywhere; in the Library (no session), every automation and every place it runs.
 * The AI writes them (/newautomation); the owner turns them on and off.
 */
export function AutomationsView({ session, sessions }: { session?: Session; sessions?: Session[] }) {
  const [list, setList] = useState<Automation[] | null>(null);
  const [tick, setTick] = useState(0);
  const [open, setOpen] = useState<string | null>(null);

  useEffect(() => {
    let stale = false;
    const load = () =>
      api.automations().then(
        (r) => !stale && setList(r.automations),
        () => !stale && setList((cur) => cur ?? []),
      );
    void load();
    const t = window.setInterval(() => document.visibilityState === 'visible' && void load(), 3000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [tick]);

  const reload = () => setTick((t) => t + 1);
  const toggle = async (a: Automation, on: boolean, where?: string) => {
    try {
      await api.automationAction(a.name, on ? 'start' : 'stop', a.scope === 'session' ? where : undefined);
      toast(`${a.title} ${on ? 'is on' : 'is off'}.`, 'info');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not change it.');
    }
    reload();
  };
  const remove = async (a: Automation) => {
    if (!(await ask({ title: `Delete ${a.title}?`, body: 'It stops everywhere it runs, and its folder is removed for good.', confirm: 'Delete', danger: true }))) return;
    try {
      await api.deleteAutomation(a.name);
      toast(`${a.title} deleted.`, 'info');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not delete it.');
    }
    reload();
  };
  const newOne = () => commandInPrompt('/newautomation');

  if (!list) return <div className="au-wrap" role="status" aria-label="Reading the automations" />;

  const here = session ? list.filter((a) => a.scope === 'session') : list;
  const everywhere = session ? list.filter((a) => a.scope === 'global') : [];

  if (list.length === 0) {
    return (
      <div className="au-wrap au-empty-wrap">
        <div className="au-empty">
          <span className="au-empty-art" aria-hidden="true">
            <i />
            <i />
            <i />
          </span>
          <h2>No automations yet</h2>
          <p>Scripts Orla runs for you: watching, checking, approving by a rule. They cost no tokens.</p>
          {session && (
            <button type="button" className="btn btn-primary" onClick={newOne}>
              <Plus />
              New automation
            </button>
          )}
        </div>
      </div>
    );
  }

  const row = (a: Automation) => (
    <AutomationRow
      key={a.name}
      a={a}
      session={session}
      sessions={sessions}
      open={open === a.name}
      setOpen={(v) => setOpen(v ? a.name : null)}
      toggle={toggle}
      remove={remove}
    />
  );

  return (
    <div className="au-wrap">
      <div className="au-col">
        <div className="au-head">
          <div>
            <h2>{session ? 'Automations' : 'All automations'}</h2>
            {!session && <p>Every automation, and where each one runs.</p>}
          </div>
          {session && (
            <button type="button" className="btn" onClick={newOne}>
              <Plus />
              New automation
            </button>
          )}
        </div>
        {here.length > 0 && (
          <section className="au-section">
            {session && <h3>With this session</h3>}
            <ul className="au-list">{here.map(row)}</ul>
          </section>
        )}
        {everywhere.length > 0 && (
          <section className="au-section">
            <h3>Everywhere</h3>
            <ul className="au-list">{everywhere.map(row)}</ul>
          </section>
        )}
      </div>
    </div>
  );
}

const STATE: Record<AutomationStatus['state'], string> = { running: 'Running', waiting: 'Waiting', stopped: 'Off', failed: 'Failed' };

function trigger(a: Automation) {
  const t = a.trigger;
  if (t.kind === 'always') return 'Always on';
  if (t.kind === 'every') return `Every ${t.text.replace(/^every\s+/i, '')}`;
  if (t.kind === 'on') return `On ${t.on}`;
  return t.text;
}

function AutomationRow({
  a,
  session,
  sessions,
  open,
  setOpen,
  toggle,
  remove,
}: {
  a: Automation;
  session?: Session;
  sessions?: Session[];
  open: boolean;
  setOpen: (v: boolean) => void;
  toggle: (a: Automation, on: boolean, where?: string) => Promise<void>;
  remove: (a: Automation) => Promise<void>;
}) {
  // Where this row speaks of: the session in view, or everywhere for a global one.
  const where = a.scope === 'global' ? '' : session?.id;
  const inst = where === undefined ? undefined : a.instances.find((i) => (i.session ?? '') === where);
  const on = where === undefined ? a.on.length > 0 : a.on.includes(where);
  const state = inst?.state ?? (on ? 'waiting' : 'stopped');
  const places = where === undefined ? a.instances : [];
  const [busy, setBusy] = useState(false);
  const flip = async () => {
    setBusy(true);
    await toggle(a, !on, where);
    setBusy(false);
  };
  return (
    <li className="au-row" data-open={open || undefined} data-state={state}>
      <div className="au-main">
        <button type="button" className="au-name" aria-expanded={open} onClick={() => setOpen(!open)}>
          <ChevronRight />
          <span className="au-mark" data-state={a.problem ? 'failed' : state}>
            {state === 'running' ? <span className="wave" /> : null}
          </span>
          <span className="au-text">
            <span className="au-title">{a.title}</span>
            <span className="au-desc">{a.problem ? `Cannot run: ${a.problem}` : a.description || a.run}</span>
          </span>
        </button>
        <span className="au-trigger">{trigger(a)}</span>
        {where !== undefined ? (
          <button
            type="button"
            role="switch"
            aria-checked={on}
            aria-label={`${a.title} ${on ? 'on' : 'off'}`}
            className="sk-switch"
            disabled={busy || !!a.problem}
            onClick={() => void flip()}
          />
        ) : (
          <span className="au-count num">{a.instances.length > 0 ? `${a.instances.length} running` : 'Off'}</span>
        )}
      </div>
      {open && (
        <div className="au-detail">
          {where !== undefined ? (
            <Instance a={a} where={where} inst={inst} />
          ) : places.length === 0 ? (
            <p className="au-note">{a.scope === 'session' ? 'Not on in any session. Turn it on from a session’s Automations tab.' : 'Off.'}</p>
          ) : (
            places.map((i) => (
              <div key={i.session ?? ''} className="au-place">
                <h4>{i.session ? sessionName(sessions?.find((s) => s.id === i.session) ?? { cwd: i.session }) : 'Everywhere'}</h4>
                <Instance a={a} where={i.session ?? ''} inst={i} />
                <button type="button" className="fl-act" onClick={() => void toggle(a, false, i.session)}>
                  Turn off here
                </button>
              </div>
            ))
          )}
          {a.scope === 'global' && where === undefined && (
            <button type="button" className="fl-act" onClick={() => void toggle(a, a.on.length === 0)}>
              {a.on.length === 0 ? 'Turn on' : 'Turn off'}
            </button>
          )}
          <div className="fl-actions">
            <button type="button" className="fl-act" onClick={() => commandInPrompt(`/editautomation ${a.name}`)} disabled={!session}>
              <Pencil />
              Edit
            </button>
            <button type="button" className="fl-act fl-act-delete" onClick={() => void remove(a)}>
              <Trash />
              Delete
            </button>
          </div>
        </div>
      )}
    </li>
  );
}

/** How an automation stands in one place, and what it printed there, as it prints it. */
function Instance({ a, where, inst }: { a: Automation; where: string; inst?: AutomationStatus }) {
  const [lines, setLines] = useState<AutomationLine[]>([]);
  const last = useRef(0);
  const box = useRef<HTMLDivElement>(null);
  const now = useNow(5000);
  const on = !!inst;
  // Turned on or off, the log is read again from the start: the live one, or the file's.
  useEffect(() => {
    last.current = 0;
    setLines([]);
  }, [a.name, where, on]);
  useEffect(() => {
    let stale = false;
    const load = () =>
      api.automationLogs(a.name, where || undefined, last.current).then(
        (r) => {
          if (stale || r.lines.length === 0) return;
          last.current = r.lines[r.lines.length - 1]!.n;
          setLines((cur) => [...cur, ...r.lines].slice(-500));
        },
        () => {},
      );
    void load();
    // Off, the log does not change; on, it is followed.
    const t = on ? window.setInterval(load, 1500) : undefined;
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [a.name, where, on]);
  const log = (
    <div className="au-log mono" ref={box} role="log" aria-label={`${a.title} log`}>
      {lines.length === 0 ? (
        <span className="au-log-empty">Nothing printed yet.</span>
      ) : (
        [...lines].reverse().map((l) => (
          <div key={l.n} className="au-line" data-stream={l.stream}>
            <span className="au-line-t num">{new Date(l.time).toLocaleString([], { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })}</span>
            <span className="au-line-x">{l.text}</span>
          </div>
        ))
      )}
    </div>
  );
  if (!inst)
    return (
      <div className="au-instance">
        <p className="au-note">{lines.length > 0 ? 'Off here. What it printed the last time it ran:' : 'Off here. Its log starts when it is turned on.'}</p>
        {lines.length > 0 && log}
      </div>
    );
  const ago = (iso?: string) => {
    if (!iso) return '';
    const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
    return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.floor(s / 60)}m ago` : `${Math.floor(s / 3600)}h ago`;
  };
  return (
    <div className="au-instance">
      <p className="au-state" data-state={inst.state}>
        {STATE[inst.state]}
        {inst.state === 'running' && inst.since ? ` · since ${ago(inst.since)}` : ''}
        {inst.state !== 'running' && inst.last_run ? ` · last ran ${ago(inst.last_run)}` : ''}
        {` · ${inst.runs} ${inst.runs === 1 ? 'run' : 'runs'}`}
        {inst.error ? ` · ${inst.error}` : ''}
      </p>
      {log}
    </div>
  );
}
