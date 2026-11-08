import { toast } from './Toast';
import { Loader } from './Loader';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { api, ApiError } from '@/lib/api';
import { folderName, isHome, sessionName } from '@/lib/format';
import { useNow, useReview, useSessionTimeline } from '@/lib/hooks';
import type { Session } from '@/lib/types';
import { ChangesView } from './Changes';
import { ArrowLeft, PanelRight, TerminalIcon } from './Icons';
import { PromptBox, type Outgoing } from './PromptBox';
import { RunBlock } from './Run';
import { Typed } from './Typed';
import { Bolt, DiffIcon, FlowIcon, Fork, PuzzleIcon, TimelineIcon } from './Icons';
import { SkillsView } from './SkillsView';
import { FlowView, useFlowRunning } from './FlowView';
import { AutomationsView } from './AutomationsView';

export function SessionView({
  session,
  onBack,
  onChanged,
  openTerminal,
  toggleDock,
  dockOn,
  openSession,
}: {
  session: Session;
  onBack: () => void;
  onChanged: () => void;
  openTerminal: (id: string) => void;
  toggleDock: () => void;
  dockOn: boolean;
  /** Opens another session, such as a fork just made. */
  openSession: (id: string) => void;
}) {
  const { timeline, live, loaded } = useSessionTimeline(session.id);
  // A fork shows the conversation it goes on from, up to the answer it was made from.
  const parent = useSessionTimeline(session.fork_parent);
  const inherited = session.fork_parent
    ? parent.timeline.runs.slice(0, Math.max(0, parent.timeline.runs.findIndex((r) => r.id === session.fork_run) + 1) || parent.timeline.runs.length)
    : [];
  const [chapters, toggleChapter] = useChapters(session.id);
  const fork = session.native_session
    ? async (run: string) => {
        try {
          const f = await api.forkSession(session.id, run);
          openSession(f.id);
        } catch (err) {
          toast(err instanceof Error ? err.message : 'The fork did not reach Orla.');
        }
      }
    : undefined;
  const now = useNow(1000);
  const feed = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  // Whatever the list knew about when the session opened is history; later steps arrive live.
  const [liveAfter] = useState(session.last_seq);
  const [cancelling, setCancelling] = useState(false);
  // The view in front stays there across a reload: back on Changes if that is where it was.
  const tabKey = `orla.tab.${session.id}`;
  type Tab = 'timeline' | 'changes' | 'flow' | 'skills' | 'automations';
  const [tab, setTabState] = useState<Tab>(() => {
    try {
      const v = localStorage.getItem(tabKey);
      return v === 'changes' || v === 'skills' || v === 'flow' || v === 'automations' ? v : 'timeline';
    } catch {
      return 'timeline';
    }
  });
  const setTab = (t: Tab) => {
    // Whatever the tab, the prompt is where typing goes.
    setTimeout(() => document.querySelector<HTMLTextAreaElement>('.composer textarea')?.focus(), 0);
    setTabState(t);
    try {
      localStorage.setItem(tabKey, t);
    } catch {}
  };
  // The highlight under the tabs slides to the one in front.
  const tabsRef = useRef<HTMLDivElement>(null);
  const [ink, setInk] = useState<React.CSSProperties>({ opacity: 0 });
  useLayoutEffect(() => {
    const el = tabsRef.current?.querySelector<HTMLElement>('.head-tab[aria-pressed="true"]');
    if (!el) return;
    const x = el.offsetLeft;
    const w = el.offsetWidth;
    // Only when it moved: setting it every render would render forever.
    setInk((old) => (old.width === w && old.transform === `translateX(${x}px)` ? old : { transform: `translateX(${x}px)`, width: w, opacity: 1 }));
  });
  const review = useReview(session.id, timeline.filesSeq);
  const changes = review.changes;
  const reviewable = changes?.git === true || Boolean(session.worktree);
  const changedCount = changes?.git ? changes.files.length + changes.more : 0;

  const latest = timeline.runs[timeline.runs.length - 1];
  const title = session.title || (isHome(session.cwd) ? sessionName(session) : latest?.prompt || session.last_run?.prompt || folderName(session.cwd));
  const runningInTerminal = latest?.status === 'running' && latest.interactive;

  // Follow the bottom while the reader is there; leave them alone once they scroll up.
  // Away from the bottom, a button brings the reader back, and swells when something new came.
  const [away, setAway] = useState(false);
  const [fresh, setFresh] = useState(false);
  useLayoutEffect(() => {
    const el = feed.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
    else if (el) setFresh(true);
  }, [timeline]);
  // Back from Changes, the feed is drawn anew: it opens on the newest message, not the first.
  useLayoutEffect(() => {
    const el = feed.current;
    if (tab !== 'timeline' || !el) return;
    pinned.current = true;
    setAway(false);
    el.scrollTop = el.scrollHeight;
  }, [tab]);
  const [scrolled, setScrolled] = useState(false);
  // Back on the timeline, it opens where the reader left it: at the newest, if they were there.
  useLayoutEffect(() => {
    const el = feed.current;
    if (tab === 'timeline' && el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [tab]);
  const onScroll = () => {
    const el = feed.current;
    if (!el) return;
    setScrolled(el.scrollTop > 4);
    pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    setAway(!pinned.current);
    if (pinned.current) setFresh(false);
  };
  const toEnd = () => {
    const el = feed.current;
    if (!el) return;
    pinned.current = true;
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.scrollTo({ top: el.scrollHeight, behavior: reduce ? 'auto' : 'smooth' });
    setFresh(false);
  };

  // Messages sent while Claude Code works wait here, in order, and go when the run ends.
  const working = Boolean(session.active_run) || latest?.status === 'running';
  const [queue, enqueue, dequeue] = useQueue(session.id);
  // The Timeline tab says the AI is working, from any tab; and that it finished, until seen.
  const [unseen, setUnseen] = useState(false);
  const wasWorking = useRef(working);
  useEffect(() => {
    if (wasWorking.current && !working && tab !== 'timeline') setUnseen(true);
    wasWorking.current = working;
  }, [working, tab]);
  useEffect(() => {
    if (tab === 'timeline') setUnseen(false);
  }, [tab]);
  // The Automations tab rolls while one of them runs its command for this session.
  const automating = useAutomating(session.id);
  // The Flow tab does the same for a flow being followed.
  const flowing = useFlowRunning(session, timeline.runs);
  const [flowUnseen, setFlowUnseen] = useState(false);
  const wasFlowing = useRef(flowing);
  useEffect(() => {
    if (wasFlowing.current && !flowing && tab !== 'flow') setFlowUnseen(true);
    wasFlowing.current = flowing;
  }, [flowing, tab]);
  useEffect(() => {
    if (tab === 'flow') setFlowUnseen(false);
  }, [tab]);
  const sendingQueued = useRef(false);
  useEffect(() => {
    const next = queue[0];
    if (!next || working || sendingQueued.current) return;
    sendingQueued.current = true;
    api
      .startRun(session.id, {
        prompt: next.o.prompt,
        attachments: next.o.attachments,
        ultracode: next.o.ultracode,
        model: next.o.model,
        effort: next.o.effort,
        permission_mode: next.o.mode,
      })
      .then(() => {
        dequeue(next.id);
        onChanged();
        toEnd();
      })
      .catch((err) => toast(err instanceof Error ? `The queued message did not go: ${err.message}` : 'The queued message did not go.'))
      .finally(() => {
        sendingQueued.current = false;
      });
  }, [queue, working, session.id]);

  // Esc stops the run from anywhere on the page, unless something there takes it first (a menu,
  // a dialog, a field being edited).
  useEffect(() => {
    if (!working) return;
    const on = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      const el = document.activeElement;
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) return;
      if (document.querySelector('dialog[open], [role="menu"], [role="dialog"]')) return;
      void cancel();
    };
    addEventListener('keydown', on);
    return () => removeEventListener('keydown', on);
  });

  // Stopping is immediate, as in Claude Code: the stop button, Esc, or Enter to send now.
  const cancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancel(session.id);
    } catch {
      // the run may have ended meanwhile; the stream says how
    } finally {
      setCancelling(false);
      onChanged();
    }
  };

  return (
    <>
      <div className="topbar">
        <button className="icon-btn" onClick={onBack} aria-label="All sessions">
          <ArrowLeft />
        </button>
        <span className="title">{sessionName(session)}</span>
        {session.terminal && (
          <button className="icon-btn" onClick={() => openTerminal(session.terminal!)} aria-label="Open its terminal">
            <TerminalIcon />
          </button>
        )}
      </div>
      <header className="head">
        <div style={{ minWidth: 0 }}>
          <h1 className="head-title">
            <Typed text={title} />
          </h1>
          <div className="head-tabs" role="group" aria-label="Show" ref={tabsRef}>
            <span className="head-tab-ink" style={ink} aria-hidden="true" />
            <button className="head-tab" data-icon="timeline" aria-pressed={tab === 'timeline'} onClick={() => setTab('timeline')}>
              <span className="head-tab-icon" data-working={working || undefined}>
                {working ? <span className="wave" /> : <TimelineIcon />}
                {unseen && <span className="head-tab-done" aria-label="Finished" />}
              </span>
              Timeline
            </button>
            {reviewable && (
              <button className="head-tab" data-icon="changes" aria-pressed={tab === 'changes'} onClick={() => setTab('changes')}>
                <DiffIcon />
                Changes
                {changedCount > 0 && <span className="count num">{changedCount}</span>}
              </button>
            )}
            <button className="head-tab" data-icon="skills" aria-pressed={tab === 'skills'} onClick={() => setTab('skills')}>
              <PuzzleIcon />
              Skills
              {(session.skills_off?.length ?? 0) > 0 && <span className="count num">{session.skills_off!.length} off</span>}
            </button>
            <button className="head-tab" data-icon="flow" aria-pressed={tab === 'flow'} onClick={() => setTab('flow')}>
              <span className="head-tab-icon" data-working={flowing || undefined}>
                {flowing ? <span className="wave" /> : <FlowIcon />}
                {flowUnseen && <span className="head-tab-done" aria-label="Finished" />}
              </span>
              Flow
            </button>
            <button className="head-tab" data-icon="automations" aria-pressed={tab === 'automations'} onClick={() => setTab('automations')}>
              <span className="head-tab-icon" data-working={automating || undefined}>
                {automating ? <span className="wave" /> : <Bolt />}
              </span>
              Automations
            </button>
          </div>
        </div>
        <div className="head-actions">
          <button className="icon-btn" onClick={toggleDock} aria-label={dockOn ? 'Hide terminals' : 'Show terminals'} aria-pressed={dockOn}>
            <PanelRight />
          </button>
        </div>
      </header>

      <div className="stage" data-float={tab === 'flow' || undefined}>
      <div className="tab-stage" key={tab} data-from={tab === 'timeline' ? 'left' : 'right'}>
      {tab === 'automations' ? (
        <AutomationsView session={session} />
      ) : tab === 'flow' ? (
        <FlowView session={session} runs={timeline.runs} onChanged={onChanged} />
      ) : tab === 'skills' ? (
        <SkillsView session={session} onChanged={onChanged} />
      ) : tab === 'changes' && reviewable ? (
        <ChangesView
          session={session}
          review={review}
          working={Boolean(session.active_run) || latest?.status === 'running'}
          onChanged={onChanged}
          onAsk={async (prompt) => {
            await api.startRun(session.id, { prompt });
            onChanged();
            setTab('timeline');
          }}
          filesSeq={timeline.filesSeq}
        />
      ) : (
      <div className="feed" ref={feed} onScroll={onScroll} data-scrolled={scrolled || undefined} aria-live="polite" aria-busy={!live && timeline.runs.length === 0}>
        {inherited.map((r) => (
          <RunBlock key={`p-${r.id}`} run={r} ctx={{ session: session.fork_parent!, cwd: session.cwd, now, liveAfter: Infinity, onAnswered: onChanged, openTerminal, inherited: true }} />
        ))}
        {session.fork_parent && (
          <div className="fork-mark">
            <Fork />
            <span>
              Forked here from{' '}
              <button type="button" className="link" onClick={() => openSession(session.fork_parent!)}>
                the original session
              </button>
              {timeline.runs.length === 0 && '. What comes next is only in this one.'}
            </span>
          </div>
        )}
        {!loaded && <Loader label="Loading the session" />}
        {loaded && timeline.runs.length === 0 && !session.fork_parent && <p className="loading">No messages yet.</p>}
        {Object.keys(chapters).length > 1 && <ChapterIndex chapters={chapters} runs={timeline.runs.map((r) => r.id)} />}
        {timeline.runs.map((r, i) => (
          <RunBlock
            key={r.id}
            run={r}
            ctx={{
              session: session.id,
              cwd: session.cwd,
              now,
              liveAfter,
              onAnswered: onChanged,
              openTerminal,
              fork,
              chapter: chapters[r.id],
              toggleChapter: (title) => toggleChapter(r.id, title),
            }}
          />
        ))}
        {queue.map((q) => (
          <div key={q.id} className="queued">
            <p className="prompt-text">{q.o.prompt}</p>
            <div className="queued-meta">
              <span>Queued · goes when this run ends</span>
              {working && (
                <button type="button" className="link" onClick={() => void api.cancel(session.id).then(onChanged, () => {})}>
                  Stop the run and send now
                </button>
              )}
              <button type="button" className="link" onClick={() => dequeue(q.id)}>
                Remove
              </button>
            </div>
          </div>
        ))}
        {away && <ToEnd fresh={fresh} onClick={toEnd} />}
      </div>
      )}
      </div>

      <Composer
        session={session}
        busy={working}
        inTerminal={Boolean(runningInTerminal)}
        closed={session.worktree?.removed ? 'The worktree was removed. Start a new session to go on.' : ''}
        onStarted={() => {
          onChanged();
          toEnd();
        }}
        onQueue={(o) => {
          enqueue(o);
          toEnd();
        }}
        onStop={() => void cancel()}
        queued={queue.length}
      />
      </div>
    </>
  );
}

function Composer({
  session,
  busy,
  inTerminal,
  closed,
  onStarted,
  onQueue,
  onStop,
  queued,
}: {
  session: Session;
  busy: boolean;
  inTerminal: boolean;
  /** Why the session takes no more prompts, if it does not. */
  closed: string;
  onStarted: () => void;
  onQueue: (o: Outgoing) => void;
  onStop: () => void;
  queued: number;
}) {
  const send = async (o: Outgoing) => {
    try {
      await api.startRun(session.id, {
        prompt: o.prompt,
        attachments: o.attachments,
        ultracode: o.ultracode,
        model: o.model,
        effort: o.effort,
        permission_mode: o.mode,
      });
    } catch (err) {
      throw err instanceof ApiError && err.status === 409 ? new Error('A run is already in progress in this session. Wait for it, or stop it.') : err;
    }
    onStarted();
  };
  return (
    <div className="composer">
      <PromptBox
        key={session.id}
        draftKey={`orla.draft.${session.id}`}
        placeholder={inTerminal ? 'The agent is working in its terminal' : busy ? (queued > 0 ? 'Enter to stop and send the queued message' : 'Enter to queue, Esc to stop') : 'Reply'}
        onSend={send}
        busy={busy}
        closed={closed || undefined}
        onQueue={onQueue}
        onStop={inTerminal ? undefined : onStop}
        queued={queued}
        cwd={session.cwd}
      />
    </div>
  );
}

const chapterKey = (session: string) => `orla.chapters.${session}`;

/** Runs the owner pinned as chapters of a session, with their titles. Kept in this browser. */
function useChapters(session: string): [Record<string, string>, (run: string, title: string) => void] {
  const [chapters, setChapters] = useState<Record<string, string>>(() => {
    try {
      return JSON.parse(localStorage.getItem(chapterKey(session)) ?? '{}') as Record<string, string>;
    } catch {
      return {};
    }
  });
  const toggle = (run: string, title: string) =>
    setChapters((old) => {
      const next = { ...old };
      if (next[run]) delete next[run];
      else next[run] = title;
      try {
        localStorage.setItem(chapterKey(session), JSON.stringify(next));
      } catch {}
      return next;
    });
  return [chapters, toggle];
}

/** The chapters of a session, in order, to jump to. */
function ChapterIndex({ chapters, runs }: { chapters: Record<string, string>; runs: string[] }) {
  const list = runs.filter((id) => chapters[id]);
  if (list.length === 0) return null;
  return (
    <nav className="chapters" aria-label="Chapters">
      {list.map((id) => (
        <button
          key={id}
          type="button"
          className="chapter-link"
          onClick={() => document.getElementById(`run-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })}
        >
          {chapters[id]}
        </button>
      ))}
    </nav>
  );
}

/** Back to the newest: an arrow, and a small swell of the sea when something new arrived. */
function ToEnd({ fresh, onClick }: { fresh: boolean; onClick: () => void }) {
  return (
    <div className="to-end-wrap">
      <button type="button" className="to-end" data-fresh={fresh || undefined} onClick={onClick} aria-label={fresh ? 'New messages: go to the newest' : 'Go to the newest'}>
        <span className="to-end-sea" aria-hidden="true">
          <span>~</span>
          <span>≈</span>
          <span>~</span>
        </span>
        <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" className="to-end-arrow">
          <path d="M8 3v10M3.5 8.5 8 13l4.5-4.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {fresh && <span className="to-end-label">New</span>}
      </button>
    </div>
  );
}

/** Messages waiting for the run in progress to end, kept in this browser so a reload keeps them. */
function useQueue(session: string): [{ id: string; o: Outgoing }[], (o: Outgoing, first?: boolean) => void, (id: string) => void] {
  const key = `orla.queue.${session}`;
  const [queue, setQueue] = useState<{ id: string; o: Outgoing }[]>(() => {
    try {
      return JSON.parse(localStorage.getItem(key) ?? '[]') as { id: string; o: Outgoing }[];
    } catch {
      return [];
    }
  });
  const save = (next: { id: string; o: Outgoing }[]) => {
    try {
      if (next.length) localStorage.setItem(key, JSON.stringify(next));
      else localStorage.removeItem(key);
    } catch {}
    return next;
  };
  const enqueue = (o: Outgoing, first = false) =>
    setQueue((q) => {
      const item = { id: `${Date.now()}-${q.length}`, o };
      return save(first ? [item, ...q] : [...q, item]);
    });
  const dequeue = (id: string) => setQueue((q) => save(q.filter((x) => x.id !== id)));
  return [queue, enqueue, dequeue];
}

/** Is an automation of this session (or a global one) running its command now. */
function useAutomating(session: string): boolean {
  const [on, setOn] = useState(false);
  useEffect(() => {
    let stale = false;
    const load = () =>
      api.automations().then(
        (r) => !stale && setOn(r.automations.some((a) => a.instances.some((i) => i.state === 'running' && (!i.session || i.session === session)))),
        () => {},
      );
    void load();
    const t = window.setInterval(() => document.visibilityState === 'visible' && void load(), 5000);
    return () => {
      stale = true;
      window.clearInterval(t);
    };
  }, [session]);
  return on;
}
