'use client';

import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError } from '@/lib/api';
import { ago, permissionModes, sessionName, toolSummary } from '@/lib/format';
import type { PendingApproval, Repo, Session } from '@/lib/types';
import { AskActions, timeLeft } from './Ask';
import { InputPreview } from './Run';
import { ArrowLeft, Inbox } from './Icons';

export function ApprovalsPage({
  approvals,
  sessions,
  now,
  onAnswered,
  openSession,
  onBack,
}: {
  approvals: PendingApproval[];
  sessions: Session[];
  now: number;
  onAnswered: () => void;
  openSession: (id: string) => void;
  onBack: () => void;
}) {
  const byId = new Map(sessions.map((s) => [s.id, s]));
  return (
    <>
      <div className="topbar">
        <button className="icon-btn" onClick={onBack} aria-label="All sessions">
          <ArrowLeft />
        </button>
        <span className="title">Approvals</span>
      </div>
      <div className="page">
        <div className="page-head">
          <div>
            <h1>Waiting for you</h1>
            <p>Requests from every session. The first answer counts, here or in the terminal.</p>
          </div>
        </div>
        {approvals.length === 0 ? (
          <div className="empty">
            <Inbox />
            <h2>Nothing is waiting for you</h2>
            <p>When an agent needs permission to use a tool, the request shows up here.</p>
          </div>
        ) : (
          <div className="approval-list">
            {approvals.map((a) => {
              const s = byId.get(a.session);
              return (
                <div key={a.approval_id} className="approval">
                  <div className="approval-what">
                    <strong>{a.tool}</strong>
                    <code>{toolSummary(a.input, s?.cwd)}</code>
                  </div>
                  <div className="approval-meta">
                    <button className="link" style={{ color: 'inherit' }} onClick={() => openSession(a.session)}>
                      {s ? sessionName(s) : a.session}
                    </button>
                    <span>{ago(a.asked_at, now)}</span>
                    <span>{timeLeft(a.expires_at, now)}</span>
                    {a.terminal && <span>also asked in the terminal</span>}
                  </div>
                  <InputPreview name={a.tool} input={a.input} cwd={s?.cwd} named />
                  <AskActions session={a.session} approval={a.approval_id} onAnswered={onAnswered} />
                </div>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}

export function NewSessionPage({
  suggestedCwd,
  started,
  openedTerminal,
  onBack,
}: {
  suggestedCwd: string;
  started: (session: string) => void;
  openedTerminal: (terminal: string) => void;
  onBack: () => void;
}) {
  const [cwd, setCwd] = useState(suggestedCwd);
  const [prompt, setPrompt] = useState('');
  const [mode, setMode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [repo, setRepo] = useState<Repo | null>(null);
  const [isolated, setIsolated] = useState(false);
  const [branch, setBranch] = useState('');

  // Whether the folder is in a git repository decides whether a worktree is on offer.
  useEffect(() => {
    const path = cwd.trim();
    setRepo(null);
    if (!path.startsWith('/')) return;
    let stale = false;
    const t = setTimeout(() => {
      api
        .gitInfo(path)
        .then((r) => !stale && setRepo(r.git && r.repo ? r.repo : null))
        .catch(() => {});
    }, 350);
    return () => {
      stale = true;
      clearTimeout(t);
    };
  }, [cwd]);

  const fail = (err: unknown) =>
    setError(err instanceof ApiError || err instanceof Error ? err.message : 'Orla did not answer. Try again.');

  // The folder the agent works in: the one typed, or the same folder in a new worktree.
  const workFolder = async () => {
    if (!(repo && isolated)) return cwd.trim();
    const made = await api.createWorktree(cwd.trim(), branch.trim() || undefined);
    return made.cwd;
  };

  const start = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const s = await api.createSession(await workFolder());
      if (prompt.trim()) await api.startRun(s.id, { prompt: prompt.trim(), permission_mode: mode || undefined });
      started(s.id);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  const inTerminal = async () => {
    setBusy(true);
    setError('');
    try {
      const t = await api.openTerminal({ argv: ['claude'], cwd: (await workFolder()) || undefined });
      openedTerminal(t.id);
    } catch (err) {
      fail(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <div className="topbar">
        <button className="icon-btn" onClick={onBack} aria-label="Back">
          <ArrowLeft />
        </button>
        <span className="title">New session</span>
      </div>
      <div className="page">
        <div className="page-head">
          <div>
            <h1>New session</h1>
            <p>Claude Code works in a folder on the server, with your own login.</p>
          </div>
        </div>
        <form className="new-form" onSubmit={start}>
          <div className="form-row">
            <label htmlFor="cwd">Folder on the server</label>
            <input id="cwd" className="field mono" required value={cwd} onChange={(e) => setCwd(e.target.value)} placeholder="/home/you/work/project" spellCheck={false} />
            <span className="hint">An absolute path. The session's runs and terminals start there.</span>
          </div>
          {repo && (
            <div className="form-row">
              <label className="check">
                <input type="checkbox" checked={isolated} onChange={(e) => setIsolated(e.target.checked)} />
                Work in a new worktree
              </label>
              <span className="hint">
                Claude Code gets a branch and a folder of its own, made from <code>{repo.branch || repo.head?.slice(0, 7)}</code>. Review
                and merge its work from the session's Changes tab.
              </span>
            </div>
          )}
          {repo && isolated && (
            <div className="form-row">
              <label htmlFor="branch">Branch</label>
              <input
                id="branch"
                className="field mono"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                placeholder="orla/quiet-river"
                spellCheck={false}
              />
              <span className="hint">
                Leave it empty and Orla makes up a name under <code>orla/</code>.
              </span>
            </div>
          )}
          <div className="form-row">
            <label htmlFor="first-prompt">First prompt</label>
            <textarea id="first-prompt" className="field" value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder="What should Claude Code do?" />
          </div>
          <div className="form-row">
            <label htmlFor="new-mode">Permissions</label>
            <select id="new-mode" className="select" style={{ width: 'fit-content' }} value={mode} onChange={(e) => setMode(e.target.value)}>
              {permissionModes.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </select>
            <span className="hint">“Ask before each tool” sends every permission request to you here.</span>
          </div>
          <div className="form-actions">
            <button className="btn btn-primary" disabled={busy || !cwd.trim()}>
              {busy ? 'Starting…' : prompt.trim() ? 'Start session' : 'Create session'}
            </button>
            <span className="or">or</span>
            <button type="button" className="btn" onClick={() => void inTerminal()} disabled={busy}>
              Open Claude Code in a terminal
            </button>
          </div>
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
        </form>
      </div>
    </>
  );
}

export function HomePage({ sessions, pending, newSession, openApprovals }: { sessions: Session[]; pending: number; newSession: () => void; openApprovals: () => void }) {
  return (
    <div className="page">
      <div className="empty">
        <h2>{sessions.length === 0 ? 'No sessions yet' : 'Pick a session'}</h2>
        <p>
          {sessions.length === 0
            ? 'Start Claude Code in a folder on the server, from here or in a terminal.'
            : pending > 0
              ? `${pending} ${pending === 1 ? 'request is' : 'requests are'} waiting for you.`
              : 'Choose one on the left to follow it live.'}
        </p>
        <div className="actions">
          {pending > 0 && (
            <button className="btn btn-primary" onClick={openApprovals}>
              Review requests
            </button>
          )}
          <button className={pending > 0 ? 'btn' : 'btn btn-primary'} onClick={newSession}>
            New session
          </button>
        </div>
      </div>
    </div>
  );
}

