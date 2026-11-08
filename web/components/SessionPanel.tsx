import '@/styles/session-settings.css';
import { ask } from './ConfirmDialog';
import { RunningFlows } from './FlowView';
import { useEffect, useState, type ReactNode } from 'react';
import { api, type ContextUsage } from '@/lib/api';
import type { Review } from '@/lib/hooks';
import type { RunView, Timeline } from '@/lib/timeline';
import type { Automation, AutomationLine, AutomationStatus, Session, TerminalInfo } from '@/lib/types';
import { BitbucketMark, Branch, Check, ChevronRight, Cross, GitHubMark, GitLabMark, Plus, RepoIcon, Spinner, Stop, TerminalIcon } from './Icons';
import { Tooltip } from './Tooltip';

// What a session's right column shows besides its terminals: where it works (Remote Control,
// repository, branch), how full the conversation is, and what runs.

export interface SettingsData {
  session: Session;
  timeline: Timeline;
  review: Review;
  terminals: TerminalInfo[];
  onChanged: () => void;
}

const repoName = (web: string) => web.replace(/^https:\/\/[^/]+\//, '');

/** The forge a repository lives on, by its host: its mark and its name, or a plain repo. */
function forge(web: string): { icon: ReactNode; name: string } {
  const host = web.replace(/^https:\/\//, '').split('/')[0]!.toLowerCase();
  if (host === 'github.com' || host.endsWith('.github.com')) return { icon: <GitHubMark />, name: 'GitHub' };
  if (host === 'gitlab.com' || host.startsWith('gitlab.')) return { icon: <GitLabMark />, name: 'GitLab' };
  if (host === 'bitbucket.org' || host.startsWith('bitbucket.')) return { icon: <BitbucketMark />, name: 'Bitbucket' };
  return { icon: <RepoIcon />, name: host };
}

function useCopy(): [string | undefined, (key: string, text: string) => void] {
  const [copied, setCopied] = useState<string>();
  const copy = (key: string, text: string) =>
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(key);
      setTimeout(() => setCopied(undefined), 1300);
    });
  return [copied, copy];
}

/** The claude.ai page of a conversation Remote Control was turned on for, if any. */
function useRemote(session: Session) {
  const [remote, setRemote] = useState<string>();
  useEffect(() => {
    let stale = false;
    api.sessionDetail(session.id).then((d) => !stale && setRemote(d.remote || undefined), () => {});
    return () => {
      stale = true;
    };
  }, [session.id, session.native_session]);
  return remote;
}

// ---------------------------------------------------------------------------------------------
// Pieces of the dialog.

// ---------------------------------------------------------------------------------------------
// Activity: how full the conversation is, and what runs.

const windowOf = (model?: string) => (model && /haiku|4-5|4-6/.test(model) ? 200_000 : 1_000_000);

function lastWithContext(runs: RunView[]) {
  for (let i = runs.length - 1; i >= 0; i--) if (runs[i]!.context) return runs[i]!;
  return undefined;
}

function useActivity(d: SettingsData) {
  const run = lastWithContext(d.timeline.runs);
  const used = run?.context ?? 0;
  const total = windowOf(run?.model);
  const share = Math.min(1, used / total);
  const tone = share > 0.85 ? 'fail' : share > 0.6 ? 'wait' : 'ok';
  const [compacting, setCompacting] = useState(false);
  const latest = d.timeline.runs[d.timeline.runs.length - 1];
  const running = latest?.status === 'running' && !latest.interactive;
  const shells =
    latest?.status === 'running'
      ? latest.steps.flatMap((s) =>
          s.kind === 'tool' && s.name === 'Bash' && (s.input as { run_in_background?: boolean })?.run_in_background
            ? [{ id: s.callId, command: String((s.input as { command?: string }).command ?? '') }]
            : [],
        )
      : [];
  const terminals = d.terminals.filter((t) => t.id === d.session.terminal || t.cwd === d.session.cwd || t.cwd.startsWith(`${d.session.cwd}/`));

  const compact = async () => {
    setCompacting(true);
    try {
      await api.startRun(d.session.id, { prompt: '/compact' });
      d.onChanged();
    } finally {
      setCompacting(false);
    }
  };

  return { run, used, total, share, tone, compacting, compact, running, shells, terminals };
}

// What fills the context, in Claude Code's own categories and colors.
const PART_COLOR: Record<string, string> = {
  Messages: '#4f8ff7',
  'System tools': '#e8753a',
  Skills: '#2fb7a0',
  'System prompt': '#e0a020',
  'MCP server instructions': '#e0559a',
  'MCP tools': '#9b7bf0',
  'Memory files': '#8fb4c7',
  'Autocompact buffer': '#5b6870',
  'Free space': '#22343c',
};
const ORDER = ['Messages', 'System tools', 'Skills', 'System prompt', 'MCP server instructions', 'MCP tools', 'Memory files', 'Autocompact buffer', 'Free space'];
const k = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(n % 1e6 ? 1 : 0)}M` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : String(n));

/** /context of the session, asked again only when a run has ended since. */
function useContextUsage(d: SettingsData, open: boolean) {
  const [usage, setUsage] = useState<ContextUsage | null>(null);
  const [state, setState] = useState<'idle' | 'loading' | 'error'>('idle');
  const runs = d.timeline.runs;
  const last = runs[runs.length - 1];
  const settled = last?.status !== 'running';
  const key = `${runs.length}:${last?.id ?? ''}:${settled}`;
  useEffect(() => {
    if (!open || !settled) return;
    let stale = false;
    setState('loading');
    api.context(d.session.id).then(
      (u) => {
        if (stale) return;
        setUsage(u);
        setState('idle');
      },
      () => !stale && setState('error'),
    );
    return () => {
      stale = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [d.session.id, key, open]);
  return { usage, state };
}

function Fold({ label, count, children }: { label: string; count: number; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  if (count === 0) return null;
  return (
    <div className="cx-fold">
      <button type="button" className="cx-fold-head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <ChevronRight />
        <span>{label}</span>
        <span className="cx-num num">{count}</span>
      </button>
      {open && <div className="cx-fold-body">{children}</div>}
    </div>
  );
}


/** Open or folded, remembered per section between visits. */
function usePersistedOpen(key: string, initial: boolean): [boolean, (v: boolean) => void] {
  const [open, setOpen] = useState(() => {
    try {
      const v = localStorage.getItem(key);
      return v === null ? initial : v === '1';
    } catch {
      return initial;
    }
  });
  const set = (v: boolean) => {
    setOpen(v);
    try {
      localStorage.setItem(key, v ? '1' : '0');
    } catch {}
  };
  return [open, set];
}

/** A part of the panel: its title folds it, and the line beside the title says or does a little. */
function Section({ title, open, setOpen, aside, children }: { title: string; open: boolean; setOpen: (v: boolean) => void; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="ps" data-open={open || undefined}>
      <div className="ps-head">
        <button type="button" className="ps-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          <span>{title}</span>
          <ChevronRight />
        </button>
        {aside && <span className="ps-aside">{aside}</span>}
      </div>
      {open && <div className="ps-body">{children}</div>}
    </section>
  );
}

/** Rows of the size the context's categories take, while Claude Code is asked for them. */
function ContextSkeleton() {
  return (
    <div className="cx-skel" role="status" aria-label="Asking Claude Code for the context">
      {[62, 48, 40, 54, 36, 44].map((w, i) => (
        <span key={i} style={{ animationDelay: `${i * 90}ms` }}>
          <i />
          <b style={{ width: `${w}%` }} />
        </span>
      ))}
    </div>
  );
}

export function ContextGroup({ d }: { d: SettingsData }) {
  const a = useActivity(d);
  const [open, setOpen] = usePersistedOpen('orla.context.open', false);
  const { usage, state } = useContextUsage(d, open);
  // Claude Code's count when there is one; the last response's own figures until then.
  const used = usage?.used ?? a.used;
  const total = usage?.total ?? a.total;
  const share = Math.min(1, total ? used / total : 0);
  const tone = share > 0.85 ? 'fail' : share > 0.6 ? 'wait' : 'ok';
  const cats = (usage?.categories ?? []).filter((c) => !c.name.includes('(deferred)')).sort((x, y) => ORDER.indexOf(x.name) - ORDER.indexOf(y.name));
  const deferred = (usage?.categories ?? []).filter((c) => c.name.includes('(deferred)'));
  const free = usage?.categories.find((c) => c.name === 'Free space')?.tokens;

  return (
    <Section
      title="Context"
      open={open}
      setOpen={setOpen}
      aside={
        !open && (
          <span className="cx-mini">
            <span className="num">{Math.round(share * 100)}%</span>
            <span className="cx-minibar" data-tone={tone}>
              <span style={{ transform: `scaleX(${share})` }} />
            </span>
          </span>
        )
      }
    >
        <div className="cx-body">
          <div className="cx-total">
            <span className="cx-pct num">{Math.round(share * 100)}% used</span>
            <span className="cx-dim num">
              {k(used)} / {k(total)}
            </span>
          </div>
          <div className="cx-bar" aria-hidden="true">
            {cats.length > 0 ? (
              cats.map((c) => <span key={c.name} style={{ flexGrow: c.tokens, background: PART_COLOR[c.name] ?? '#5b6870' }} />)
            ) : (
              <span style={{ flexGrow: 1, background: 'var(--press)' }} />
            )}
          </div>
          {state === 'loading' && !usage && <ContextSkeleton />}
          {state === 'error' && !usage && <p className="cx-dim">Claude Code did not answer. It shows up after a run.</p>}
          {cats.length > 0 && (
            <ul className="cx-list">
              {cats.map((c) => (
                <li key={c.name}>
                  <span className="cx-swatch" style={{ background: PART_COLOR[c.name] ?? '#5b6870' }} />
                  <span className="cx-name">{c.name}</span>
                  <span className="cx-num num">{k(c.tokens)}</span>
                  <span className="cx-num cx-pctcol num">{(c.percent ?? 0).toFixed(1)}%</span>
                </li>
              ))}
            </ul>
          )}
          {deferred.length > 0 && (
            <ul className="cx-list cx-deferred">
              {deferred.map((c) => (
                <li key={c.name}>
                  <span className="cx-swatch" />
                  <span className="cx-name">{c.name}</span>
                  <span className="cx-num num">{k(c.tokens)}</span>
                  <span className="cx-num cx-pctcol">—</span>
                </li>
              ))}
            </ul>
          )}
          {usage && (
            <>
              <Fold label="MCP tools" count={usage.mcp_tools?.length ?? 0}>
                {usage.mcp_tools?.map((t) => (
                  <div key={t.name} className="cx-item">
                    <span className="cx-name">{t.name.replace(/^mcp__/, '').replace(/__/g, ' · ').replace(/_/g, ' ')}</span>
                    <span className="cx-num num">{k(t.tokens)}</span>
                  </div>
                ))}
              </Fold>
              <Fold label="Skills" count={usage.skills?.length ?? 0}>
                {usage.skills?.map((t) => (
                  <div key={t.name + t.detail} className="cx-item">
                    <span className="cx-name">{t.name}</span>
                    <span className="cx-dim">{t.detail}</span>
                    <span className="cx-num num">{k(t.tokens)}</span>
                  </div>
                ))}
              </Fold>
            </>
          )}
          <div className="cx-foot">
            <span className="cx-dim num">{free !== undefined ? `${k(free)} until auto-compact` : ''}</span>
            <button type="button" className="btn btn-sm" onClick={() => void a.compact()} disabled={!a.run || a.compacting || Boolean(d.session.active_run)}>
              {a.compacting ? 'Compacting…' : 'Compact session'}
            </button>
          </div>
        </div>
    </Section>
  );
}

/** Where the session works: Remote Control, repository, branch. */
export function EnvironmentGroup({ d }: { d: SettingsData }) {
  const remote = useRemote(d.session);
  const [copied, copy] = useCopy();
  const repo = d.review.changes?.git ? d.review.changes.repo : undefined;
  const [open, setOpen] = usePersistedOpen('orla.panel.environment', true);
  if (!remote && !repo?.web && !repo?.branch) return null;
  return (
    <Section title="Environment" open={open} setOpen={setOpen} aside={!open && repo?.web ? <span className="ps-note">{repoName(repo.web)}</span> : undefined}>
      <div className="env-list">
        {remote && (
          <Tooltip label="Open in claude.ai" side="bottom">
            <a className="env-row" href={remote} target="_blank" rel="noreferrer noopener">
              <span className="env-live" aria-hidden="true" />
              <span>Remote Control</span>
            </a>
          </Tooltip>
        )}
        {repo?.web && (
          <Tooltip label={`Open on ${forge(repo.web).name}`} side="bottom">
            <a className="env-row" href={repo.web} target="_blank" rel="noreferrer noopener">
              {forge(repo.web).icon}
              <span>{repoName(repo.web)}</span>
            </a>
          </Tooltip>
        )}
        {repo?.branch && (
          <Tooltip label={copied === 'branch' ? 'Copied!' : 'Copy branch name'} side="bottom">
            <button type="button" className="env-row" data-done={copied === 'branch' || undefined} onClick={() => copy('branch', repo.branch!)}>
              {copied === 'branch' ? <Check /> : <Branch />}
              <span>{repo.branch}</span>
            </button>
          </Tooltip>
        )}
      </div>
    </Section>
  );
}

export function RunningGroup({ d, open, newTerminal }: { d: SettingsData; open?: (id: string) => void; newTerminal?: (argv: string[]) => void }) {
  const a = useActivity(d);
  const empty = !a.running && a.shells.length === 0 && a.terminals.length === 0;
  const count = (a.running ? 1 : 0) + a.shells.length + a.terminals.length;
  const [shown, setShown] = usePersistedOpen('orla.panel.running', true);
  const close = async (t: TerminalInfo) => {
    const name = t.argv.length ? t.argv[0]!.split('/').pop()! : 'shell';
    if (!(await ask({ title: `Close ${name}?`, body: 'Every process it started ends with it.', confirm: 'Close' }))) return;
    await api.closeTerminal(t.id).then(d.onChanged, () => {});
  };
  return (
    <Section
      title="Running"
      open={shown}
      setOpen={setShown}
      aside={
        <>
          {!shown && count > 0 && <span className="ps-count num">{count}</span>}
          {newTerminal && (
            <Tooltip label="New shell" side="bottom">
              <button type="button" className="icon-btn" onClick={() => newTerminal([])} aria-label="New shell">
                <Plus />
              </button>
            </Tooltip>
          )}
        </>
      }
    >
      <div className="ps-rows">
        {empty && <p className="ps-empty">Nothing running</p>}
        {a.running && (
          <div className="ss-task">
            <Spinner />
            <span className="ss-task-name">Run in progress</span>
            <Tooltip label="Stop" side="bottom">
              <button type="button" className="ss-task-act" onClick={() => void api.cancel(d.session.id).then(d.onChanged, () => {})} aria-label="Stop the run">
                <Stop />
              </button>
            </Tooltip>
          </div>
        )}
        {a.running && <RunningFlows session={d.session} runs={d.timeline.runs} />}
        {a.shells.map((sh) => (
          <div key={sh.id} className="ss-task">
            <span className="ss-dot" />
            <code className="ss-task-name">{sh.command}</code>
          </div>
        ))}
        {a.terminals.map((t) => (
          <div key={t.id} className="ss-task">
            <TerminalIcon />
            {open ? (
              <button type="button" className="ss-task-name ss-task-open" onClick={() => open(t.id)}>
                {t.argv.length ? t.argv.map((x, i) => (i === 0 ? x.split('/').pop() : x)).join(' ') : 'Shell'}
              </button>
            ) : (
              <span className="ss-task-name">{t.argv.length ? t.argv.map((x, i) => (i === 0 ? x.split('/').pop() : x)).join(' ') : 'Shell'}</span>
            )}
            <Tooltip label="Close" side="bottom">
              <button type="button" className="ss-task-act" onClick={() => void close(t)} aria-label="Close terminal">
                <Cross />
              </button>
            </Tooltip>
          </div>
        ))}
      </div>
    </Section>
  );
}


/** The session's own panel in the right column. */
export function SessionPanel({ d, open, newTerminal }: { d: SettingsData; open: (id: string) => void; newTerminal: (argv: string[]) => void }) {
  return (
    <div className="sp-panel">
      <EnvironmentGroup d={d} />
      <ContextGroup d={d} />
      <RunningGroup d={d} open={open} newTerminal={newTerminal} />
      <AutomationsGroup d={d} />
    </div>
  );
}

const AUTO_STATE: Record<AutomationStatus['state'], string> = { running: 'Running', waiting: 'Waiting', stopped: 'Off', failed: 'Failed' };

/** The automations on for this session, and the global ones on: how each stands, at a glance. */
export function AutomationsGroup({ d }: { d: SettingsData }) {
  const [list, setList] = useState<Automation[]>([]);
  const [shown, setShown] = usePersistedOpen('orla.panel.automations', true);
  const [open, setOpen] = useState<string | null>(null);
  useEffect(() => {
    let stale = false;
    const load = () => api.automations().then((r) => !stale && setList(r.automations), () => {});
    void load();
    const t = window.setInterval(() => document.visibilityState === 'visible' && void load(), 3000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, []);
  const mine = list.flatMap((a) => {
    const where = a.scope === 'global' ? '' : d.session.id;
    if (!a.on.includes(where)) return [];
    const inst = a.instances.find((i) => (i.session ?? '') === where);
    return [{ a, where, state: (inst?.state ?? 'stopped') as AutomationStatus['state'], inst }];
  });
  const failing = mine.filter((m) => m.state === 'failed').length;
  return (
    <Section
      title="Automations"
      open={shown}
      setOpen={setShown}
      aside={!shown && mine.length > 0 ? <span className="ps-count num" data-tone={failing ? 'fail' : undefined}>{mine.length}</span> : undefined}
    >
      <div className="ps-rows">
        {mine.length === 0 && <p className="ps-empty">None on</p>}
        {mine.map(({ a, where, state, inst }) => {
          const key = `${a.name}/${where}`;
          return (
            <div key={key} className="rf" data-open={open === key || undefined}>
              <button type="button" className="ss-task rf-head" aria-expanded={open === key} onClick={() => setOpen(open === key ? null : key)}>
                <Tooltip label={`${AUTO_STATE[state]}${where === '' ? ' · everywhere' : ''}${inst?.error ? ` · ${inst.error}` : ''}`} side="bottom">
                  {state === 'running' ? <span className="wave" /> : <span className="pa-dot" data-state={state} />}
                </Tooltip>
                <span className="ss-task-name pa-name">{a.title}</span>
                <ChevronRight />
              </button>
              {open === key && <LastLines name={a.name} where={where} />}
            </div>
          );
        })}
      </div>
    </Section>
  );
}

/** The last lines an automation printed, kept fresh while shown. */
function LastLines({ name, where }: { name: string; where: string }) {
  const [lines, setLines] = useState<AutomationLine[]>([]);
  useEffect(() => {
    let stale = false;
    const load = () => api.automationLogs(name, where || undefined, 0).then((r) => !stale && setLines(r.lines.slice(-6)), () => {});
    void load();
    const t = window.setInterval(load, 2000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [name, where]);
  return (
    <div className="pa-log mono">
      {lines.length === 0 ? (
        <span className="pa-log-empty">Nothing printed yet</span>
      ) : (
        [...lines].reverse().map((l) => (
          <div key={l.n} className="pa-line" data-stream={l.stream}>
            {l.text}
          </div>
        ))
      )}
    </div>
  );
}
