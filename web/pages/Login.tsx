import '@fontsource/lilex/400.css';
import '@fontsource/lilex/500.css';
import '@fontsource/lilex/700.css';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { api, Unauthorized } from '@/lib/api';
import { ARRIVAL_KEY } from '@/components/Arrival';
import { Lockup } from '@/components/Logo';
import { RepoBadge } from '@/components/RepoBadge';
import { Sea, type SeaState } from '@/components/Sea';
import { toast, Toaster } from '@/components/Toast';

const FAILED = {
  token: 'That is not this server’s token. Copy it again from the token file.',
  down: 'Orla did not answer. Check that it is running and try again.',
};

export default function Login() {
  const [token, setToken] = useState('');
  const [state, setState] = useState<SeaState>('idle');
  const [host, setHost] = useState('');
  const [theme, setTheme] = useState('blue');
  const form = useRef<HTMLFormElement>(null);
  useEffect(() => {
    setHost(window.location.host);
    // Written by the settings screen; blue until someone picks another.
    try {
      const saved = localStorage.getItem('orla-theme');
      if (saved === 'black' || saved === 'white') setTheme(saved);
    } catch {}
  }, []);
  const busy = state === 'busy' || state === 'ok';

  const fail = (kind: keyof typeof FAILED) => {
    toast(FAILED[kind]);
    setState('error');
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setState('busy');
    try {
      await api.login(token.trim());
      setState('ok');
      // The tide covers the screen, then the panel opens under the same water (see Arrival).
      const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
      try {
        if (!reduce) sessionStorage.setItem(ARRIVAL_KEY, '1');
      } catch {}
      setTimeout(() => {
        history.replaceState(null, '', '/');
        dispatchEvent(new PopStateEvent('popstate'));
      }, reduce ? 0 : 900);
    } catch (err) {
      fail(err instanceof Unauthorized ? 'token' : 'down');
    }
  };

  return (
    <main className="login" data-state={state} data-theme={theme}>
      <Sea state={state} above={form} />
      <div className="login-side">
        <Lockup height={34} />
        <div>
          <h1>Your agents are waiting on the shore.</h1>
          <p className="login-lead">Paste the access token</p>
        </div>
        <form ref={form} onSubmit={submit}>
          <label className="visually-hidden" htmlFor="token">
            Access token
          </label>
          <div className="login-row">
            <input
              id="token"
              type="password"
              autoComplete="current-password"
              autoFocus
              required
              readOnly={busy}
              aria-invalid={state === 'error'}
              value={token}
              onChange={(e) => {
                setToken(e.target.value);
                if (state === 'error') setState('idle');
              }}
              placeholder="orla_…"
              spellCheck={false}
            />
            <button disabled={busy || !token.trim()}>{busy ? 'Logging in…' : 'Log in'}</button>
          </div>
        </form>
      </div>

      <Toaster name="login.log" />

      <footer className="login-foot">
        <RepoBadge />
        <span className="login-addr" aria-hidden="true">
          {host}
        </span>
      </footer>
    </main>
  );
}
