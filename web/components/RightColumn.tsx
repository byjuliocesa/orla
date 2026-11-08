import { toast } from './Toast';
import { Gauge, TerminalIcon } from './Icons';
import { Tooltip } from './Tooltip';
import { useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { useReview, useSessionTimeline } from '@/lib/hooks';
import type { Session, TerminalInfo } from '@/lib/types';
import { SessionPanel } from './SessionPanel';
import { TerminalDock } from './TerminalDock';

/**
 * The right column beside a session, as in Claude Code: the session's own panel (context,
 * pull request, what runs) and its terminals, one at a time under two tabs.
 */
export function RightColumn({
  session,
  terminals,
  terminal,
  cwd,
  select,
  changed,
  onBack,
}: {
  session: Session;
  terminals: TerminalInfo[];
  /** The terminal in view, if one was opened. */
  terminal?: string;
  cwd?: string;
  select: (id: string | undefined) => void;
  changed: () => void;
  onBack: () => void;
}) {
  // The tab in front is kept across reloads. A terminal in the address does not move it: only
  // opening one now does.
  const [tab, setTabState] = useState<'session' | 'terminals'>(() => {
    try {
      return localStorage.getItem('orla.rc.tab') === 'terminals' ? 'terminals' : 'session';
    } catch {
      return 'session';
    }
  });
  const setTab = (t: 'session' | 'terminals') => {
    setTabState(t);
    try {
      localStorage.setItem('orla.rc.tab', t);
    } catch {}
  };
  const seen = useRef(terminal);
  useEffect(() => {
    if (terminal && terminal !== seen.current) setTab('terminals');
    seen.current = terminal;
  }, [terminal]);
  const { timeline } = useSessionTimeline(session.id);
  const review = useReview(session.id, timeline.filesSeq);

  const open = (id: string) => {
    select(id);
    setTab('terminals');
  };
  const newTerminal = async (argv: string[]) => {
    try {
      const t = await api.openTerminal({ argv, cwd: cwd ?? session.cwd });
      changed();
      open(t.id);
    } catch (err) {
      toast(err instanceof Error ? err.message : 'The terminal did not open.');
    }
  };

  return (
    <aside className="rc" aria-label="Session side panel">
      <div className="rc-tabs" role="tablist">
        <Tooltip label="Session" side="bottom">
          <button type="button" role="tab" className="rc-tab" data-icon="session" aria-label="Session" aria-selected={tab === 'session'} onClick={() => setTab('session')}>
            <Gauge />
          </button>
        </Tooltip>
        <Tooltip label="Terminals" side="bottom">
          <button type="button" role="tab" className="rc-tab" data-icon="terminals" aria-label="Terminals" aria-selected={tab === 'terminals'} onClick={() => setTab('terminals')}>
            <TerminalIcon />
            {terminals.length > 0 && <span className="rc-count num">{terminals.length}</span>}
          </button>
        </Tooltip>
      </div>
      {tab === 'session' ? (
        <SessionPanel d={{ session, timeline, review, terminals, onChanged: changed }} open={open} newTerminal={(argv) => void newTerminal(argv)} />
      ) : (
        <TerminalDock terminals={terminals} active={terminal ?? session.terminal} cwd={cwd} select={select} changed={changed} onBack={onBack} />
      )}
    </aside>
  );
}
