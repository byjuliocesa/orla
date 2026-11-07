import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { api, ApiError } from '@/lib/api';
import { between, clock, duration, folderName, modelName, permissionModes, sessionName, shortPath, tokens } from '@/lib/format';
import { useMedia, useNow, useReview, useSessionTimeline } from '@/lib/hooks';
import type { Session } from '@/lib/types';
import { ChangesView } from './Changes';
import { ArrowLeft, Branch, PanelRight, Send, Stop, TerminalIcon } from './Icons';
import { RunBlock } from './Run';

export function SessionView({
  session,
  onBack,
  onChanged,
  openTerminal,
  toggleDock,
  dockOn,
}: {
  session: Session;
  onBack: () => void;
  onChanged: () => void;
  openTerminal: (id: string) => void;
  toggleDock: () => void;
  dockOn: boolean;
}) {
  const { timeline, live } = useSessionTimeline(session.id);
  const now = useNow(1000);
  const feed = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  // Whatever the list knew about when the session opened is history; later steps arrive live.
  const [liveAfter] = useState(session.last_seq);
  const [cancelling, setCancelling] = useState(false);
  const [tab, setTab] = useState<'timeline' | 'changes'>('timeline');
  const review = useReview(session.id, timeline.filesSeq);
  const changes = review.changes;
  const reviewable = changes?.git === true || Boolean(session.worktree);
  const changedCount = changes?.git ? changes.files.length + changes.more : 0;
  const branch = changes?.git ? changes.repo.branch : undefined;
  // Once its worktree is gone, the session is shown in the repository it came from.
  const folder = session.worktree?.removed ? session.worktree.repo : session.cwd;

  const latest = timeline.runs[timeline.runs.length - 1];
  const title = latest?.prompt || session.last_run?.prompt || folderName(session.cwd);
  const runningInTerminal = latest?.status === 'running' && latest.interactive;
  const narrow = useMedia('(max-width: 860px)');

  // What the latest run is doing, kept in view while the feed scrolls.
  let progress = '';
  let used = 0;
  if (latest) {
    const ms =
      latest.status === 'running'
        ? now - Date.parse(latest.started)
        : (latest.usage?.durationMs ?? between(latest.started, latest.finished));
    if (ms > 0) progress = latest.status === 'running' ? `running ${clock(ms)}` : `took ${duration(ms)}`;
    const t = latest.usage ?? latest.live;
    used = t.input + t.output + t.cacheRead + t.cacheWrite;
  }

  // Follow the bottom while the reader is there; leave them alone once they scroll up.
  useLayoutEffect(() => {
    const el = feed.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [timeline]);
  const onScroll = () => {
    const el = feed.current;
    if (el) pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  const cancel = async () => {
    if (!window.confirm('Cancel this run? Claude Code stops, and every process it started ends.')) return;
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
          <h1 className="head-title">{title}</h1>
          <div className="head-meta">
            <span className="path" title={folder}>
              {shortPath(folder, narrow ? 30 : 48)}
            </span>
            {branch && (
              <span className="branch" title={`Branch ${branch}`}>
                <Branch /> {branch}
              </span>
            )}
            <span className="agent">{session.agent === 'claude' ? 'Claude Code' : session.agent}</span>
            {latest?.model && <span>{modelName(latest.model)}</span>}
            {progress && <span className="num">{progress}</span>}
            {used > 0 && <span className="num">{tokens(used)} tokens</span>}
            {session.terminal && (
              <button className="link" onClick={() => openTerminal(session.terminal!)}>
                in {session.terminal.slice(0, 13)}
              </button>
            )}
            {!live && <span>reconnecting…</span>}
          </div>
          {reviewable && (
            <div className="head-tabs" role="group" aria-label="Show">
              <button className="head-tab" aria-pressed={tab === 'timeline'} onClick={() => setTab('timeline')}>
                Timeline
              </button>
              <button className="head-tab" aria-pressed={tab === 'changes'} onClick={() => setTab('changes')}>
                Changes
                {changedCount > 0 && <span className="count num">{changedCount}</span>}
              </button>
            </div>
          )}
        </div>
        <div className="head-actions">
          {session.active_run && (
            <button className="btn" onClick={() => void cancel()} disabled={cancelling} aria-label="Cancel run">
              <Stop />{' '}
              <span>
                {cancelling ? 'Cancelling…' : 'Cancel'}
                <span className="long">{cancelling ? '' : ' run'}</span>
              </span>
            </button>
          )}
          <button className="icon-btn" onClick={toggleDock} aria-label={dockOn ? 'Hide terminals' : 'Show terminals'} aria-pressed={dockOn}>
            <PanelRight />
          </button>
        </div>
      </header>

      {tab === 'changes' && reviewable ? (
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
      <div className="feed" ref={feed} onScroll={onScroll} aria-live="polite" aria-busy={!live && timeline.runs.length === 0}>
        {timeline.runs.length === 0 && (
          <p className="loading">{live ? 'No runs yet. Send the first prompt below.' : 'Loading the session…'}</p>
        )}
        {timeline.runs.map((r, i) => (
          <RunBlock
            key={r.id}
            run={r}
            limits={i === timeline.runs.length - 1 ? timeline.limits : undefined}
            ctx={{ session: session.id, cwd: session.cwd, now, liveAfter, onAnswered: onChanged, openTerminal }}
          />
        ))}
      </div>
      )}

      <Composer
        session={session}
        busy={Boolean(session.active_run) || Boolean(runningInTerminal)}
        inTerminal={Boolean(runningInTerminal)}
        closed={session.worktree?.removed ? 'The worktree was removed. Start a new session to go on.' : ''}
        onStarted={onChanged}
      />
    </>
  );
}

function Composer({
  session,
  busy,
  inTerminal,
  closed,
  onStarted,
}: {
  session: Session;
  busy: boolean;
  inTerminal: boolean;
  /** Why the session takes no more prompts, if it does not. */
  closed: string;
  onStarted: () => void;
}) {
  const [prompt, setPrompt] = useState('');
  const [mode, setMode] = useState('');
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const box = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 240)}px`;
  }, [prompt]);

  const send = async (e?: FormEvent) => {
    e?.preventDefault();
    const text = prompt.trim();
    if (!text || busy || sending || closed) return;
    setSending(true);
    setError('');
    try {
      await api.startRun(session.id, { prompt: text, permission_mode: mode || undefined });
      setPrompt('');
      onStarted();
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 409
          ? 'A run is already in progress in this session. Wait for it, or cancel it.'
          : err instanceof Error
            ? err.message
            : 'The prompt did not reach Orla.',
      );
    } finally {
      setSending(false);
    }
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) void send();
  };

  // The run's own status line already says when one is in progress.
  const note = closed || (inTerminal ? 'Claude Code is working in its terminal; answer it there or wait for it.' : '');

  return (
    <form className="composer" onSubmit={send}>
      <div className="composer-box">
        <label className="visually-hidden" htmlFor="prompt">
          Prompt
        </label>
        <textarea
          id="prompt"
          ref={box}
          rows={1}
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={onKey}
          disabled={Boolean(closed)}
          placeholder={busy ? 'Write the next prompt; send it when the run ends' : 'Send a prompt to this session'}
        />
        <div className="composer-bar">
          <span className="grow">
            {note || (
              <span className="keys">
                <kbd>Ctrl</kbd> <kbd>Enter</kbd> to send
              </span>
            )}
          </span>
          {!closed && (
            <>
              <label className="visually-hidden" htmlFor="mode">
                Permissions
              </label>
              <select id="mode" className="select" value={mode} onChange={(e) => setMode(e.target.value)} title="Permissions for this run">
                {permissionModes.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
              <button className="btn btn-primary" disabled={busy || sending || Boolean(closed) || !prompt.trim()}>
                <Send /> {sending ? 'Sending…' : 'Run'}
              </button>
            </>
          )}
        </div>
      </div>
      {error && (
        <p className="form-error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
