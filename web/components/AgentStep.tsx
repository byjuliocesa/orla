import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { api } from '@/lib/api';
import type { AgentInfo } from '@/lib/types';
import { Check } from './Icons';
import { toast } from './Toast';

export const agentReady = (a: AgentInfo) => a.installed && a.loggedIn;

const said = (err: unknown, fallback: string) => (err instanceof Error && err.message ? err.message : fallback);

/**
 * The first step of the first visit: the agent Orla runs, installed and signed in here. Signing
 * in opens the agent's page in a new tab; the code it shows is pasted back here.
 */
export function AgentStep({ next, titleId }: { next: () => void; titleId: string }) {
  const [agents, setAgents] = useState<AgentInfo[] | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [login, setLogin] = useState<{ id: string; page: string } | null>(null);
  const [code, setCode] = useState('');
  const codeRef = useRef<HTMLInputElement>(null);

  const check = useCallback(async () => {
    try {
      setAgents(await api.agents());
    } catch {
      toast('Orla could not check the agent.');
    }
  }, []);
  useEffect(() => void check(), [check]);

  const update = (a: AgentInfo) => setAgents((list) => list?.map((x) => (x.id === a.id ? a : x)) ?? [a]);

  const install = async (a: AgentInfo) => {
    setBusy(a.id);
    try {
      update(await api.installAgent(a.id));
    } catch (err) {
      toast(said(err, 'The install failed.'));
    } finally {
      setBusy(null);
    }
  };

  const signIn = async (a: AgentInfo) => {
    // Opened now, in the click: a tab opened after the wait is blocked as a pop-up.
    const tab = window.open('', '_blank');
    setBusy(a.id);
    try {
      const { page } = await api.startLogin(a.id);
      if (tab) tab.location.href = page;
      setLogin({ id: a.id, page });
      setCode('');
      setTimeout(() => codeRef.current?.focus(), 0);
    } catch (err) {
      tab?.close();
      toast(said(err, 'Orla could not start the sign-in.'));
    } finally {
      setBusy(null);
    }
  };

  const finish = async (e: FormEvent) => {
    e.preventDefault();
    if (!login || !code.trim()) return;
    setBusy(login.id);
    try {
      update(await api.finishLogin(login.id, code.trim()));
      setLogin(null);
    } catch (err) {
      // A refused code ends the sign-in; the next try starts over with a new page.
      toast(said(err, 'The sign-in failed.'));
      setLogin(null);
    } finally {
      setBusy(null);
    }
  };

  const ready = !!agents?.some(agentReady);
  return (
    <section className="sy-connect" aria-busy={busy !== null}>
      <div className="sy-intro ag-intro">
        <h2 id={titleId}>Your agent</h2>
      </div>
      <ul className="ag-list">
        {agents === null && <li className="ag-row ag-skeleton" aria-label="Checking" />}
        {agents?.map((a) => {
          const state = agentReady(a) ? 'ready' : a.installed ? 'login' : 'install';
          const working = busy === a.id;
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
                login?.id !== a.id && (
                  <button
                    type="button"
                    className="btn"
                    disabled={busy !== null}
                    aria-busy={working}
                    onClick={() => void (state === 'install' ? install(a) : signIn(a))}
                  >
                    {working && <span className="ag-spin" aria-hidden="true" />}
                    {state === 'install' ? (working ? 'Installing' : 'Install') : 'Sign in'}
                  </button>
                )
              )}
            </li>
          );
        })}
      </ul>
      {login && (
        <form className="ag-code" onSubmit={(e) => void finish(e)}>
          <label className="sy-label" htmlFor="ag-code">
            Code from the{' '}
            <a href={login.page} target="_blank" rel="noreferrer">
              sign-in page
            </a>
          </label>
          <div className="sy-field">
            <input
              ref={codeRef}
              id="ag-code"
              className="sy-input"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              spellCheck={false}
              autoComplete="off"
              disabled={busy !== null}
            />
            <button type="submit" className="btn btn-primary" disabled={busy !== null || !code.trim()}>
              {busy === login.id && <span className="ag-spin" aria-hidden="true" />}
              Sign in
            </button>
          </div>
        </form>
      )}
      <div className="ag-foot">
        <button type="button" className="btn btn-primary" disabled={!ready || busy !== null} onClick={next}>
          Continue
        </button>
        {!ready && (
          <button
            type="button"
            className="ob-skip ag-skip"
            disabled={busy !== null}
            onClick={() => {
              if (login) void api.cancelLogin(login.id).catch(() => {});
              next();
            }}
          >
            Skip
          </button>
        )}
      </div>
    </section>
  );
}
