'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { HomePage, ApprovalsPage, NewSessionPage } from '@/components/Pages';
import { SessionView } from '@/components/SessionView';
import { Sidebar } from '@/components/Sidebar';
import { TerminalDock } from '@/components/TerminalDock';
import { api } from '@/lib/api';
import { folderName, toolSummary } from '@/lib/format';
import { useMedia, useNow, useOverview } from '@/lib/hooks';
import { useRoute } from '@/lib/route';

function stored(key: string): string | null {
  if (typeof window === 'undefined') return null; // prerendering
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function store(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // private mode: the preference lasts this visit
  }
}

export default function Panel() {
  const [route, go] = useRoute();
  const overview = useOverview();
  const now = useNow(15_000);
  const wide = useMedia('(min-width: 1280px)');
  const phone = useMedia('(max-width: 860px)');
  const [dockPref, setDockPref] = useState(true);
  const [notify, setNotify] = useState(false);

  useEffect(() => {
    setDockPref(stored('orla.dock') !== 'off');
    setNotify(stored('orla.notify') === 'on' && typeof Notification !== 'undefined' && Notification.permission === 'granted');
  }, []);

  const { sessions, approvals, terminals, loaded } = overview;
  const session = route.session ? sessions.find((s) => s.id === route.session) : undefined;

  // At the desk the panel opens on the session that moved last; on a phone, on the list.
  useEffect(() => {
    if (loaded && !phone && route.view === 'home' && sessions[0]) go({ view: 'session', session: sessions[0].id }, true);
  }, [loaded, phone, route.view, sessions, go]);

  // The tab title counts what waits; a notification announces a new request while away.
  const seen = useRef<Set<string> | null>(null);
  useEffect(() => {
    document.title = approvals.length > 0 ? `(${approvals.length}) Orla` : 'Orla';
    if (!loaded) return;
    const ids = new Set(approvals.map((a) => a.approval_id));
    if (seen.current && notify && document.hidden) {
      for (const a of approvals) {
        if (seen.current.has(a.approval_id)) continue;
        const s = sessions.find((x) => x.id === a.session);
        const n = new Notification(`${a.tool} needs your permission`, {
          body: `${toolSummary(a.input)}${s ? ` · ${folderName(s.cwd)}` : ''}`,
          tag: a.approval_id,
        });
        n.onclick = () => {
          window.focus();
          go({ view: 'approvals' });
        };
      }
    }
    seen.current = ids;
  }, [approvals, loaded, notify, sessions, go]);

  const toggleNotify = useCallback(async () => {
    if (notify) {
      setNotify(false);
      store('orla.notify', 'off');
      return;
    }
    if (typeof Notification === 'undefined') return;
    const permission = await Notification.requestPermission();
    const on = permission === 'granted';
    setNotify(on);
    store('orla.notify', on ? 'on' : 'off');
  }, [notify]);

  const toggleDock = useCallback(() => {
    if (!wide) {
      go({ view: 'terminals', session: route.session, terminal: session?.terminal });
      return;
    }
    setDockPref((on) => {
      store('orla.dock', on ? 'off' : 'on');
      return !on;
    });
  }, [wide, go, route.session, session?.terminal]);

  const logout = useCallback(async () => {
    await api.logout().catch(() => {});
    window.location.assign('/login/');
  }, []);

  const openTerminal = useCallback(
    (id: string) => {
      if (wide && route.view === 'session') {
        setDockPref(true);
        go({ ...route, terminal: id }, true);
      } else go({ view: 'terminals', terminal: id, session: route.session });
    },
    [wide, route, go],
  );

  const home = () => go({ view: 'home' });
  const dockBeside = wide && dockPref && route.view !== 'terminals';
  const cwd = session?.cwd ?? stored('orla.cwd') ?? '';

  let main: React.ReactNode;
  switch (route.view) {
    case 'approvals':
      main = (
        <ApprovalsPage
          approvals={approvals}
          sessions={sessions}
          now={now}
          onAnswered={overview.refresh}
          openSession={(id) => go({ view: 'session', session: id })}
          onBack={home}
        />
      );
      break;
    case 'new':
      main = (
        <NewSessionPage
          suggestedCwd={cwd}
          started={(id) => {
            overview.refresh();
            go({ view: 'session', session: id });
          }}
          openedTerminal={(id) => {
            overview.refresh();
            go({ view: 'terminals', terminal: id });
          }}
          onBack={home}
        />
      );
      break;
    case 'terminals':
      main = (
        <TerminalDock
          terminals={terminals}
          active={route.terminal}
          cwd={cwd || undefined}
          select={(id) => go({ ...route, terminal: id }, true)}
          changed={overview.refresh}
          onBack={() => (route.session ? go({ view: 'session', session: route.session }) : home())}
        />
      );
      break;
    case 'session':
      main = session ? (
        <SessionView
          key={session.id}
          session={session}
          onBack={home}
          onChanged={overview.refresh}
          openTerminal={openTerminal}
          toggleDock={toggleDock}
          dockOn={dockBeside}
        />
      ) : loaded ? (
        <div className="page">
          <div className="empty">
            <h2>This session is not here</h2>
            <p>It may belong to another Orla installation.</p>
            <button className="btn" onClick={home}>
              All sessions
            </button>
          </div>
        </div>
      ) : (
        <p className="loading">Loading…</p>
      );
      break;
    default:
      main = loaded ? (
        <HomePage sessions={sessions} pending={approvals.length} newSession={() => go({ view: 'new' })} openApprovals={() => go({ view: 'approvals' })} />
      ) : (
        <p className="loading">Loading…</p>
      );
  }

  // Remember the folder of the session in view, for the next new one.
  useEffect(() => {
    if (session?.cwd) store('orla.cwd', session.cwd);
  }, [session?.cwd]);

  return (
    <div className="app" data-view={route.view} data-dock={dockBeside ? 'on' : 'off'}>
      <Sidebar
        route={route}
        go={go}
        sessions={sessions}
        approvals={approvals}
        terminals={terminals}
        live={overview.live}
        loaded={loaded}
        now={now}
        notify={notify}
        toggleNotify={() => void toggleNotify()}
        logout={() => void logout()}
      />
      <main className="main">{main}</main>
      {dockBeside && (
        <TerminalDock
          terminals={terminals}
          active={route.terminal ?? session?.terminal}
          cwd={cwd || undefined}
          select={(id) => go({ ...route, terminal: id }, true)}
          changed={overview.refresh}
          onBack={home}
        />
      )}
    </div>
  );
}
