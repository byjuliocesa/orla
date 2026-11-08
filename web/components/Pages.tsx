import { ago, sessionName, toolSummary } from '@/lib/format';
import type { PendingApproval, Session } from '@/lib/types';
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

export function HomePage({ sessions, pending, newSession, openApprovals }: { sessions: Session[]; pending: number; newSession: () => void; openApprovals: () => void }) {
  return (
    <div className="page">
      <div className="empty">
        <h2>{sessions.length === 0 ? 'No sessions yet' : 'Pick a session'}</h2>
        <p>
          {sessions.length === 0
            ? 'Start an agent in a folder on the server, from here or in a terminal.'
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

