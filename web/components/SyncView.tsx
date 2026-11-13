import '@/styles/sync.css';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '@/lib/api';
import { ago, shortPath } from '@/lib/format';
import { useNow } from '@/lib/hooks';
import type { LibraryEntry, LibraryStatus, SavedSession } from '@/lib/types';
import { ask } from './ConfirmDialog';
import { RepoIcon, SyncIcon } from './Icons';
import { toast } from './Toast';

type Action = Parameters<typeof api.libraryAction>[0];
type Body = Parameters<typeof api.libraryAction>[1];

/** The library's status, polled while its tab is open; null until the first answer. */
export function useLibrary(): [LibraryStatus | null, (s: LibraryStatus) => void, () => void] {
  const [st, setSt] = useState<LibraryStatus | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let stale = false;
    const load = () => api.library().then((s) => !stale && setSt(s), () => {});
    void load();
    const t = window.setInterval(() => document.visibilityState === 'visible' && void load(), 5000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [tick]);
  return [st, setSt, () => setTick((t) => t + 1)];
}

/** One line on how the library stands, for the header and the connection block. */
export function syncWords(st: LibraryStatus, now: number): string {
  switch (st.state) {
    case 'off':
      return 'Not synced';
    case 'syncing':
      return 'Syncing';
    case 'conflict':
      return 'Needs you';
    case 'error':
      return 'Sync failed';
    case 'pending':
      return st.pending === 1 ? '1 change to send' : `${st.pending} changes to send`;
    default:
      return st.last_sync ? `Synced ${ago(st.last_sync, now)}` : 'Synced';
  }
}

/**
 * Sync: the library in a git repository of the owner's, so the same skills, flows and
 * automations open on every machine that runs Orla. Any remote works, with the access this
 * machine's git already has.
 */
export function SyncView({ st, setSt, reload, openSession }: { st: LibraryStatus | null; setSt: (s: LibraryStatus) => void; reload: () => void; openSession: (id: string) => void }) {
  const [busy, setBusy] = useState<string | null>(null);

  const run = async (action: Action, body?: Body, key: string = action): Promise<boolean> => {
    setBusy(key);
    try {
      setSt(await api.libraryAction(action, body));
      return true;
    } catch (err) {
      // A conflict shows in the connection block, which the reload brings in.
      if (!(err instanceof ApiError && err.status === 409)) toast(err instanceof Error ? capital(err.message) + '.' : 'Orla could not reach the library.');
      reload();
      return false;
    } finally {
      setBusy(null);
    }
  };

  if (!st) return <div className="au-wrap" role="status" aria-label="Reading the library" />;

  // Groups follow the library's folders: shared, one per agent, automations.
  const places = [{ key: 'shared', title: 'Shared' }, ...st.agents.map((a) => ({ key: a.name, title: `${a.name}/` })), { key: 'automations', title: 'Automations' }];

  return (
    <div className="au-wrap">
      <div className="au-col sy-col">
        {st.remote ? <Connected st={st} busy={busy} run={run} setSt={setSt} /> : <Connect connected={setSt} />}

        <section className="au-section">
          <div className="sy-head">
            <h3>In the library</h3>
            <span className="sy-where num" title={st.dir}>
              {shortPath(st.dir, 40)}
            </span>
          </div>
          {st.entries.length === 0 ? (
            <p className="au-note sy-empty">Empty. Move items in from below.</p>
          ) : (
            places.map((p) => {
              const items = st.entries.filter((e) => e.place === p.key);
              if (items.length === 0) return null;
              return (
                <div key={p.key} className="sy-place">
                  <h4 className={p.key !== 'shared' && p.key !== 'automations' ? 'sy-folder' : undefined}>{p.title}</h4>
                  <ul className="au-list">
                    {items.map((e) => (
                      <Row key={e.place + e.name} e={e}>
                        {e.place !== 'shared' && e.place !== 'automations' && (
                          <button type="button" className="fl-act" disabled={!!busy} onClick={() => void run('move', { from: e.place, name: e.name, to: 'shared' }, e.place + e.name)}>
                            Share
                          </button>
                        )}
                      </Row>
                    ))}
                  </ul>
                </div>
              );
            })
          )}
        </section>

        {st.sessions.length > 0 && (
          <section className="au-section">
            <div className="sy-head">
              <h3>Sessions</h3>
            </div>
            <ul className="au-list">
              {st.sessions.map((x) => (
                <SessionRow key={x.key} x={x} openSession={openSession} />
              ))}
            </ul>
          </section>
        )}

        {st.outside.length > 0 && (
          <section className="au-section">
            <div className="sy-head">
              <h3>Not in the library</h3>
            </div>
            <ul className="au-list">
              {st.outside.map((e) => {
                const key = e.place + e.name;
                return (
                  <Row key={key} e={e} from={e.place === 'automations' ? undefined : `${e.place}/`}>
                    {e.place === 'automations' ? (
                      <button type="button" className="fl-act" disabled={!!busy} onClick={() => void run('adopt', { from: e.place, name: e.name, to: 'automations' }, key)}>
                        Move in
                      </button>
                    ) : (
                      <>
                        <button type="button" className="fl-act" disabled={!!busy} onClick={() => void run('adopt', { from: e.place, name: e.name, to: 'shared' }, key)}>
                          Share
                        </button>
                        <button type="button" className="fl-act" disabled={!!busy} onClick={() => void run('adopt', { from: e.place, name: e.name, to: e.place }, key)}>
                          Keep separate
                        </button>
                      </>
                    )}
                  </Row>
                );
              })}
            </ul>
          </section>
        )}
      </div>
    </div>
  );
}

function Row({ e, from, children }: { e: LibraryEntry; from?: string; children?: React.ReactNode }) {
  const blocked = e.blocked ?? [];
  return (
    <li className="au-row sy-row">
      <div className="au-main">
        <span className="au-name au-name-static">
          <span className="au-text">
            <span className="au-title">
              {e.name}
              {e.kind === 'flow' && <span className="sy-kind">flow</span>}
            </span>
            {e.description && <span className="au-desc">{e.description}</span>}
          </span>
        </span>
        <span className="sy-side">
          <span className="sy-meta">
            {from ? (
              <span className="sy-from">{from}</span>
            ) : blocked.length > 0 ? (
              <span className="sy-blocked" title={`A folder named ${e.name} already exists there, so it was left alone.`}>
                Name in use
              </span>
            ) : null}
          </span>
          {children && <span className="sy-acts">{children}</span>}
        </span>
      </div>
    </li>
  );
}

/** A saved session: opened where it already is, or here, in a folder the owner confirms. */
function SessionRow({ x, openSession }: { x: SavedSession; openSession: (id: string) => void }) {
  const now = useNow(60_000);
  const [asking, setAsking] = useState(false);
  const [cwd, setCwd] = useState(x.cwd);
  const [busy, setBusy] = useState(false);
  const open = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      openSession((await api.openSaved(x.key, cwd.trim())).id);
    } catch (err) {
      toast(err instanceof Error ? capital(err.message) + '.' : 'Orla could not open it.');
      setBusy(false);
    }
  };
  return (
    <li className="au-row sy-row" data-open={asking || undefined}>
      <div className="au-main">
        <span className="au-name au-name-static">
          <span className="au-text">
            <span className="au-title">{x.title}</span>
            <span className="au-desc">
              {x.host} · {ago(x.saved, now)}
            </span>
          </span>
        </span>
        <span className="sy-side">
          <span className="sy-meta">
            <span className="sy-from" title={x.cwd}>
              {shortPath(x.cwd, 32)}
            </span>
          </span>
          <span className="sy-acts">
            {x.here ? (
              <button type="button" className="fl-act" onClick={() => openSession(x.here!)}>
                Open
              </button>
            ) : (
              <button type="button" className="fl-act" aria-expanded={asking} onClick={() => setAsking((v) => !v)}>
                Open here
              </button>
            )}
          </span>
        </span>
      </div>
      {asking && (
        <form className="sy-open" onSubmit={(e) => void open(e)}>
          <label className="sy-label" htmlFor={`sy-cwd-${x.key}`}>
            Folder on this machine
          </label>
          <div className="sy-field">
            <input id={`sy-cwd-${x.key}`} className="sy-input" value={cwd} onChange={(e) => setCwd(e.target.value)} spellCheck={false} autoComplete="off" autoFocus disabled={busy} />
            <button type="submit" className="btn btn-primary" disabled={busy || !cwd.trim()}>
              {busy ? 'Opening…' : 'Open'}
            </button>
          </div>
        </form>
      )}
    </li>
  );
}

export function Connect({
  connected,
  title = 'Sync your library',
  lead = 'With any git repository, using this machine’s git access.',
  children,
  titleId,
}: {
  connected: (s: LibraryStatus) => void;
  title?: string;
  lead?: string;
  children?: React.ReactNode;
  titleId?: string;
}) {
  const [remote, setRemote] = useState('');
  const [busy, setBusy] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const v = remote.trim();
    if (!v) {
      toast('Paste a repository address.');
      input.current?.focus();
      return;
    }
    setBusy(true);
    try {
      connected(await api.libraryAction('connect', { remote: v }));
      toast('Library synced.', 'info');
    } catch (err) {
      toast(err instanceof Error ? capital(err.message) + '.' : 'Orla could not reach the repository.');
      input.current?.focus();
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="sy-connect" onSubmit={(e) => void submit(e)} noValidate>
      <div className="sy-intro">
        <h2 id={titleId}>{title}</h2>
        <p>{lead}</p>
      </div>
      <label className="sy-label" htmlFor="sy-remote">
        Repository address
      </label>
      <div className="sy-field">
        <input
          ref={input}
          id="sy-remote"
          className="sy-input"
          value={remote}
          onChange={(e) => setRemote(e.target.value)}
          placeholder="git@github.com:you/library.git"
          spellCheck={false}
          autoComplete="off"
          disabled={busy}
        />
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Connecting…' : 'Connect'}
        </button>
      </div>
      {children}
    </form>
  );
}

function Connected({ st, busy, run, setSt }: { st: LibraryStatus; busy: string | null; run: (a: Action, b?: Body, key?: string) => Promise<boolean>; setSt: (s: LibraryStatus) => void }) {
  const now = useNow(15_000);
  const syncing = st.state === 'syncing' || busy === 'sync' || busy === 'connect';
  const host = hostOf(st.remote);
  const disconnect = async () => {
    if (!(await ask({ title: 'Stop syncing?', body: 'Nothing is deleted here or in the repository.', confirm: 'Stop syncing', danger: false }))) return;
    void run('disconnect');
  };
  const auto = async (on: boolean) => {
    try {
      setSt(await api.libraryAuto(on));
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not change it.');
    }
  };
  return (
    <section className="sy-link" data-state={syncing ? 'syncing' : st.state}>
      <div className="sy-ends">
        <div className="sy-end">
          <span className="sy-end-label">This machine</span>
          <span className="sy-end-value">{st.entries.length === 1 ? '1 item' : `${st.entries.length} items`}</span>
        </div>
        <div className="sy-wire" aria-hidden="true">
          <span className="sy-wire-line" />
          <span className="sy-wire-icon">
            <SyncIcon />
          </span>
        </div>
        <div className="sy-end sy-end-remote">
          <span className="sy-end-label">
            <RepoIcon />
            {host}
          </span>
          <span className="sy-end-value num" title={st.remote}>
            <span className="sy-path">{pathOf(st.remote)}</span>
            {st.branch && <span className="sy-branch">{st.branch}</span>}
          </span>
        </div>
      </div>

      <div className="sy-bar">
        <p className="sy-state" role="status">
          {syncing ? 'Syncing' : syncWords(st, now)}
        </p>
        <label className="sy-auto">
          <button type="button" role="switch" className="sk-switch" aria-checked={st.auto} onClick={() => void auto(!st.auto)} aria-label="Auto-sync" />
          Auto-sync
        </label>
        <button type="button" className="btn btn-primary sy-now" disabled={syncing} onClick={() => void run('sync')}>
          <SyncIcon />
          {syncing ? 'Syncing…' : 'Sync now'}
        </button>
        <button type="button" className="sy-off" disabled={!!busy} onClick={() => void disconnect()}>
          Stop syncing
        </button>
      </div>

      {st.state === 'conflict' && (
        <div className="sy-problem" role="alert">
          <p>Same lines changed here and in the repository. Nothing was overwritten.</p>
          <ul>
            {(st.conflicts ?? []).map((f) => (
              <li key={f} className="num">
                {f}
              </li>
            ))}
          </ul>
          <p>
            Resolve in <code>{st.dir}</code>, then sync.
          </p>
        </div>
      )}
      {st.state === 'error' && st.error && (
        <div className="sy-problem" role="alert">
          <p>{capital(st.error)}.</p>
        </div>
      )}

    </section>
  );
}

function capital(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** github.com, from git@github.com:o/r.git or https://github.com/o/r.git. */
function hostOf(remote: string): string {
  const scp = /^[^@/]+@([^:/]+):/.exec(remote);
  if (scp) return scp[1]!;
  try {
    return new URL(remote).host || 'Repository';
  } catch {
    return 'Repository';
  }
}

/** o/r, without the host and .git. */
function pathOf(remote: string): string {
  const scp = /^[^@/]+@[^:/]+:(.+)$/.exec(remote);
  let p = scp ? scp[1]! : remote;
  if (!scp) {
    try {
      const u = new URL(remote);
      p = u.protocol === 'file:' ? u.pathname : u.pathname.replace(/^\//, '');
    } catch {}
  }
  return p.replace(/\.git$/, '');
}
