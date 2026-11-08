import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Arrival } from '@/components/Arrival';
import { HomePage, ApprovalsPage } from '@/components/Pages';
import { LibraryView } from '@/components/LibraryView';
import { NewChat } from '@/components/NewChat';
import { SessionView } from '@/components/SessionView';
import { Sidebar } from '@/components/Sidebar';
import { TerminalDock } from '@/components/TerminalDock';
import { RightColumn } from '@/components/RightColumn';
import { Loader } from '@/components/Loader';
import { Resizer, useWidth } from '@/components/Resizer';
import { Toaster } from '@/components/Toast';
import { AskHost } from '@/components/ConfirmDialog';
import { api } from '@/lib/api';
import { sessionName, toolSummary } from '@/lib/format';
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
  // Both side columns take the width the owner drags them to.
  const [sideW, setSideW, resetSide] = useWidth('orla.width.side', 264, 200, 440);
  const [dockW, setDockW, resetDock] = useWidth('orla.width.dock', 480, 320, 820);
  const [resizing, setResizing] = useState(false);
  const overview = useOverview();
  const now = useNow(15_000);
  const wide = useMedia('(min-width: 1280px)');
  const phone = useMedia('(max-width: 860px)');
  const [dockPref, setDockPref] = useState(true);
  // Turned on from the settings screen, still to come; what was turned on before keeps working.
  const [notify, setNotify] = useState(false);
  const [rail, setRail] = useState(false);

  useEffect(() => {
    setDockPref(stored('orla.dock') !== 'off');
    setRail(stored('orla.side') === 'rail');
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
          body: `${toolSummary(a.input)}${s ? ` · ${sessionName(s)}` : ''}`,
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

  const toggleRail = useCallback(() => {
    setRail((on) => {
      store('orla.side', on ? 'open' : 'rail');
      return !on;
    });
  }, []);

  const refresh = overview.refresh;
  const actions = useMemo(() => {
    const change = (id: string, c: Parameters<typeof api.updateSession>[1]) => api.updateSession(id, c).then(refresh);
    return {
      rename: (id: string, title: string) => change(id, { title }),
      pin: (id: string, pinned: boolean) => change(id, { pinned }),
      archive: (id: string, archived: boolean) => change(id, { archived }),
      remove: (id: string) => api.deleteSession(id).then(refresh),
    };
  }, [refresh]);

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
  // New terminals start where the session works, or in its repository once its worktree is gone.
  const cwd = (session?.worktree?.removed ? session.worktree.repo : session?.cwd) ?? stored('orla.cwd') ?? '';

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
    case 'library':
      main = <LibraryView sessions={sessions} onBack={() => (route.session ? go({ view: 'session', session: route.session }) : home())} />;
      break;
    case 'new':
      main = (
        <NewChat
          started={(id) => {
            overview.refresh();
            go({ view: 'session', session: id });
          }}
          onBack={home}
          dockOn={dockBeside}
          toggleDock={toggleDock}
          canDock={wide}
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
          openSession={(id) => {
            overview.refresh();
            go({ view: 'session', session: id });
          }}
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
        <Loader label="Loading" />
      );
      break;
    default:
      main = loaded ? (
        <HomePage sessions={sessions} pending={approvals.length} newSession={() => go({ view: 'new' })} openApprovals={() => go({ view: 'approvals' })} />
      ) : (
        <Loader label="Loading" />
      );
  }

  // The Library is a page of its own: no terminals beside it.
  const showDock = dockBeside && route.view !== 'library';

  // Remember the folder of the session in view, for the next new one.
  const remembered = session?.worktree?.repo ?? session?.cwd;
  useEffect(() => {
    if (remembered) store('orla.cwd', remembered);
  }, [remembered]);

  return (
    <div
      className="app"
      data-view={route.view}
      data-dock={showDock ? 'on' : 'off'}
      data-side={rail ? 'rail' : 'open'}
      data-resizing={resizing || undefined}
      style={{ '--side': `${sideW}px`, '--dock': `${dockW}px` } as React.CSSProperties}
    >
      <Sidebar
        route={route}
        go={go}
        sessions={sessions}
        loaded={loaded}
        logout={() => void logout()}
        rail={rail}
        toggleRail={toggleRail}
        actions={actions}
      />
      <main className="main">{main}</main>
      {dockBeside && route.view === 'session' && session ? (
        <RightColumn
          session={session}
          terminals={terminals}
          terminal={route.terminal}
          cwd={cwd || undefined}
          select={(id) => go({ ...route, terminal: id }, true)}
          changed={overview.refresh}
          onBack={home}
        />
      ) : showDock && (
        <TerminalDock
          terminals={terminals}
          active={route.terminal ?? session?.terminal}
          cwd={cwd || undefined}
          select={(id) => go({ ...route, terminal: id }, true)}
          changed={overview.refresh}
          onBack={home}
        />
      )}
      {!phone && !rail && (
        <Resizer edge="left" width={sideW} setWidth={setSideW} reset={resetSide} label="Resize the sessions sidebar" style={{ left: sideW }} onDrag={setResizing} />
      )}
      {!phone && showDock && (
        <Resizer edge="right" width={dockW} setWidth={setDockW} reset={resetDock} label="Resize the right column" style={{ right: dockW }} onDrag={setResizing} />
      )}
      <Toaster />
      <AskHost />
      <Arrival />
    </div>
  );
}

