'use client';

import { useState } from 'react';
import { ApiError } from '@/lib/api';
import { sessionName } from '@/lib/format';
import type { Route } from '@/lib/route';
import type { Session } from '@/lib/types';
import { ConfirmDialog } from './ConfirmDialog';
import { LogOut, PanelLeft, Pin, Plus } from './Icons';
import { Lockup } from './Logo';
import { SessionMenu, type SessionAction } from './SessionMenu';
import { Tooltip } from './Tooltip';

/** The dot a session shows: its state, or how its last run ended when it is idle. */
export function dotState(s: Session): string {
  if (s.state === 'waiting' || s.state === 'working' || s.state === 'ended') return s.state;
  if (s.last_run?.status === 'failed') return 'failed';
  return 'idle';
}

const stateWord: Record<string, string> = { working: 'Working', waiting: 'Needs you', ended: 'Ended', failed: 'Failed', idle: '' };

export interface SessionActions {
  rename: (id: string, title: string) => Promise<unknown>;
  pin: (id: string, pinned: boolean) => Promise<unknown>;
  archive: (id: string, archived: boolean) => Promise<unknown>;
  remove: (id: string) => Promise<unknown>;
}

type Confirm = { kind: 'logout' } | { kind: 'remove'; session: Session };

export function Sidebar({
  route,
  go,
  sessions,
  loaded,
  logout,
  rail,
  toggleRail,
  actions,
}: {
  route: Route;
  go: (r: Route) => void;
  sessions: Session[];
  loaded: boolean;
  logout: () => void;
  /** Folded to its icons. */
  rail: boolean;
  toggleRail: () => void;
  actions: SessionActions;
}) {
  const [menu, setMenu] = useState<{ session: Session; x: number; y: number } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Pinned first; otherwise the server's order, newest first.
  const open = sessions.filter((s) => !s.archived).sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned));
  const archived = sessions.filter((s) => s.archived);
  const empty = loaded && open.length === 0;

  const pick = (s: Session, a: SessionAction) => {
    setMenu(null);
    if (a === 'rename') setRenaming(s.id);
    if (a === 'pin') void actions.pin(s.id, !s.pinned);
    if (a === 'archive') void actions.archive(s.id, !s.archived);
    if (a === 'copy') void navigator.clipboard?.writeText(s.worktree?.path ?? s.cwd);
    if (a === 'remove') setConfirm({ kind: 'remove', session: s });
  };

  const closeConfirm = () => {
    setConfirm(null);
    setError(null);
  };

  const doConfirm = async () => {
    if (!confirm) return;
    if (confirm.kind === 'logout') return logout();
    setBusy(true);
    try {
      await actions.remove(confirm.session.id);
      if (route.session === confirm.session.id) go({ view: 'home' });
      closeConfirm();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Orla did not answer. Try again.');
    } finally {
      setBusy(false);
    }
  };

  const row = (s: Session) => {
    const state = dotState(s);
    const name = sessionName(s);
    if (renaming === s.id) {
      return (
        <div key={s.id} className="session" data-renaming>
          <span className="dot" data-state={state} />
          <input
            className="session-rename"
            defaultValue={s.title || name}
            aria-label="Session name"
            autoFocus
            onFocus={(e) => e.currentTarget.select()}
            onKeyDown={(e) => {
              if (e.key === 'Enter') e.currentTarget.blur();
              if (e.key === 'Escape') {
                e.currentTarget.dataset.cancel = '1';
                e.currentTarget.blur();
              }
            }}
            onBlur={(e) => {
              const v = e.currentTarget.value.trim();
              setRenaming(null);
              if (!e.currentTarget.dataset.cancel && v !== name) void actions.rename(s.id, v);
            }}
            maxLength={120}
            spellCheck={false}
          />
        </div>
      );
    }
    return (
      <Tooltip
        key={s.id}
        off={!rail}
        label={
          <>
            {name}
            {stateWord[state] && <span className="tip-state" data-state={state}>{stateWord[state]}</span>}
          </>
        }
      >
        <button
          className="session"
          data-archived={s.archived ? '' : undefined}
          aria-current={route.session === s.id && route.view === 'session' ? 'page' : undefined}
          onClick={() => go({ view: 'session', session: s.id })}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ session: s, x: e.clientX, y: e.clientY });
          }}
        >
          <span className="dot" data-state={state} />
          <span className="session-name">{name}</span>
          {s.pinned && (
            <span className="session-pin" aria-label="Pinned">
              <Pin />
            </span>
          )}
          {stateWord[state] && <span className="visually-hidden">, {stateWord[state]}</span>}
        </button>
      </Tooltip>
    );
  };

  return (
    <nav className="side" aria-label="Orla">
      <div className="brand">
        <Lockup height={22} />
        <Tooltip label={rail ? 'Expand sidebar' : 'Collapse sidebar'}>
          <button className="icon-btn side-toggle" onClick={toggleRail} aria-expanded={!rail} aria-label={rail ? 'Expand sidebar' : 'Collapse sidebar'}>
            <PanelLeft />
          </button>
        </Tooltip>
      </div>

      {empty ? (
        <Tooltip label="New session" off={!rail}>
          <button className="nav-item side-new-wide" onClick={() => go({ view: 'new' })}>
            <Plus />
            <span className="label">New session</span>
          </button>
        </Tooltip>
      ) : (
        <div className="side-head">
          <span>Sessions</span>
          <Tooltip label="New session">
            <button className="icon-btn side-new" onClick={() => go({ view: 'new' })} aria-label="New session">
              <Plus />
            </button>
          </Tooltip>
        </div>
      )}

      <div className="sessions">
        {open.map(row)}
        {archived.length > 0 && (
          <>
            <button className="side-fold" aria-expanded={showArchived} onClick={() => setShowArchived((v) => !v)}>
              Archived <span>{archived.length}</span>
            </button>
            {showArchived && archived.map(row)}
          </>
        )}
      </div>

      <div className="side-foot">
        <Tooltip label="Log out" off={!rail}>
          <button className="nav-item side-logout" onClick={() => setConfirm({ kind: 'logout' })}>
            <LogOut />
            <span className="label">Log out</span>
          </button>
        </Tooltip>
      </div>

      {menu && <SessionMenu session={menu.session} x={menu.x} y={menu.y} onPick={(a) => pick(menu.session, a)} onClose={() => setMenu(null)} />}

      <ConfirmDialog
        open={confirm?.kind === 'logout'}
        title="Log out of Orla?"
        confirm="Log out"
        onConfirm={() => void doConfirm()}
        onClose={closeConfirm}
      >
        This browser forgets the login. Coming back takes the access token again.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm?.kind === 'remove'}
        title={confirm?.kind === 'remove' ? `Remove ${sessionName(confirm.session)}?` : ''}
        confirm="Remove"
        busy={busy}
        error={error}
        onConfirm={() => void doConfirm()}
        onClose={closeConfirm}
      >
        Its conversation and steps leave Orla. The folder and its files stay on the server.
      </ConfirmDialog>
    </nav>
  );
}
