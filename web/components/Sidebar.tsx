'use client';

import { ago, folderName } from '@/lib/format';
import type { Route } from '@/lib/route';
import type { PendingApproval, Session, TerminalInfo } from '@/lib/types';
import { Bell, Inbox, LogOut, Plus, TerminalIcon } from './Icons';

/** The dot a session shows: its state, or how its last run ended when it is idle. */
export function dotState(s: Session): string {
  if (s.state === 'waiting' || s.state === 'working' || s.state === 'ended') return s.state;
  if (s.last_run?.status === 'failed') return 'failed';
  return 'idle';
}

const stateWord: Record<string, string> = { working: 'working', waiting: 'needs you', ended: 'ended', failed: 'failed', idle: '' };

export function Sidebar({
  route,
  go,
  sessions,
  approvals,
  terminals,
  live,
  loaded,
  now,
  notify,
  toggleNotify,
  logout,
}: {
  route: Route;
  go: (r: Route) => void;
  sessions: Session[];
  approvals: PendingApproval[];
  terminals: TerminalInfo[];
  live: boolean;
  loaded: boolean;
  now: number;
  notify: boolean;
  toggleNotify: () => void;
  logout: () => void;
}) {
  return (
    <nav className="side" aria-label="Orla">
      <div className="brand">
        <span className="wordmark">Orla</span>
        <span className="conn" data-live={live} role="status">
          {live ? 'Live' : 'Reconnecting'}
        </span>
      </div>

      <button className="nav-item" aria-current={route.view === 'approvals' ? 'page' : undefined} onClick={() => go({ view: 'approvals' })}>
        <Inbox />
        <span className="label">Approvals</span>
        <span className="count" data-alert={approvals.length > 0}>
          {approvals.length}
        </span>
      </button>
      <button className="nav-item" aria-current={route.view === 'terminals' ? 'page' : undefined} onClick={() => go({ view: 'terminals', terminal: route.terminal })}>
        <TerminalIcon />
        <span className="label">Terminals</span>
        <span className="count">{terminals.length}</span>
      </button>

      <div className="side-head">
        <span>Sessions</span>
        <button className="icon-btn" onClick={() => go({ view: 'new' })} aria-label="New session" title="New session">
          <Plus />
        </button>
      </div>
      <div className="sessions">
        {loaded && sessions.length === 0 && <p className="side-empty">No sessions yet.</p>}
        {sessions.map((s) => {
          const state = dotState(s);
          return (
            <button
              key={s.id}
              className="session"
              aria-current={route.session === s.id && route.view === 'session' ? 'page' : undefined}
              onClick={() => go({ view: 'session', session: s.id })}
            >
              <span className="dot" data-state={state} />
              <span className="session-name">{folderName(s.cwd)}</span>
              <span className="session-meta" data-state={state}>
                {stateWord[state] || ago(s.updated ?? s.created, now)}
              </span>
              <span className="session-sub">{s.last_run?.prompt || 'No prompt yet'}</span>
            </button>
          );
        })}
      </div>

      <div className="side-foot">
        <button className="nav-item" onClick={toggleNotify} aria-pressed={notify} title="Browser notifications for permission requests">
          <Bell />
          <span className="label">{notify ? 'Notifying' : 'Notify me'}</span>
        </button>
        <button className="icon-btn" onClick={logout} aria-label="Log out" title="Log out">
          <LogOut />
        </button>
      </div>
    </nav>
  );
}
