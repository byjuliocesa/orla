import { useCallback, useEffect, useState } from 'react';
import { api } from '@/lib/api';
import type { AgentInfo } from '@/lib/types';
import { Check } from './Icons';
import { terminalFont, terminalTheme } from './TerminalDock';
import { TerminalView } from './TerminalView';
import { toast } from './Toast';

type Term = { id: string; agent: string; install: boolean };

export const agentReady = (a: AgentInfo) => a.installed && a.loggedIn;

/**
 * The first step of the first visit: the agent Orla runs, installed and signed in here. Install
 * and Sign in run in a terminal inside the step; when it ends, the agent is checked again.
 */
export function AgentStep({ next, titleId }: { next: () => void; titleId: string }) {
  const [agents, setAgents] = useState<AgentInfo[] | null>(null);
  const [term, setTerm] = useState<Term | null>(null);
  const [checking, setChecking] = useState(false);

  const check = useCallback(async () => {
    setChecking(true);
    try {
      const list = await api.agents();
      setAgents(list);
      return list;
    } catch {
      toast('Orla could not check the agent.');
      return null;
    } finally {
      setChecking(false);
    }
  }, []);
  useEffect(() => void check(), [check]);

  const run = async (a: AgentInfo, install: boolean) => {
    try {
      const t = await api.openTerminal({ argv: install ? a.install : a.login });
      setTerm({ id: t.id, agent: a.id, install });
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not open a terminal.');
    }
  };

  const ended = async (t: Term) => {
    const list = await check();
    // A command that worked leaves nothing to read; one that failed stays open to be read.
    const a = list?.find((x) => x.id === t.agent);
    if (a && (agentReady(a) || (t.install && a.installed))) {
      setTerm(null);
      void api.closeTerminal(t.id).catch(() => {});
    }
  };

  const ready = !!agents?.some(agentReady);
  return (
    <section className="sy-connect" aria-busy={checking}>
      <div className="sy-intro">
        <h2 id={titleId}>Your agent</h2>
        <p>Orla runs it on this machine.</p>
      </div>
      <ul className="ag-list">
        {agents === null && <li className="ag-row ag-skeleton" aria-label="Checking" />}
        {agents?.map((a) => {
          const state = agentReady(a) ? 'ready' : a.installed ? 'login' : 'install';
          return (
            <li key={a.id} className="ag-row" data-state={state}>
              <span className="ag-dot" aria-hidden="true" />
              <span className="ag-name">{a.name}</span>
              <span className="ag-meta">{state === 'ready' ? (a.account ?? a.version) : state === 'login' ? 'Signed out' : 'Not installed'}</span>
              {state === 'ready' ? (
                <span className="ag-ok" aria-label="Ready">
                  <Check />
                </span>
              ) : (
                <button type="button" className="btn" disabled={!!term || checking} onClick={() => void run(a, state === 'install')}>
                  {state === 'login' ? 'Sign in' : 'Install'}
                </button>
              )}
            </li>
          );
        })}
      </ul>
      {term && (
        <div className="ag-term">
          <TerminalView key={term.id} id={term.id} theme={terminalTheme} fontFamily={terminalFont} onExit={() => void ended(term)} />
        </div>
      )}
      <div className="ag-foot">
        <button type="button" className="btn btn-primary" disabled={!ready} onClick={next}>
          Continue
        </button>
        {!ready && (
          <button type="button" className="ob-skip ag-skip" onClick={next}>
            Skip
          </button>
        )}
      </div>
    </section>
  );
}
