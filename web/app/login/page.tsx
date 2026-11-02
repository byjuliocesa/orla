'use client';

import { useState, type FormEvent } from 'react';
import { api, Unauthorized } from '@/lib/api';

export default function Login() {
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.login(token.trim());
      window.location.assign('/');
    } catch (err) {
      setError(
        err instanceof Unauthorized
          ? 'That is not this server’s token. Copy it again from the token file.'
          : 'Orla did not answer. Check that it is running and try again.',
      );
      setBusy(false);
    }
  };

  return (
    <main className="login">
      <form onSubmit={submit}>
        <h1>Orla</h1>
        <p>
          Paste the access token from <code>~/.local/share/orla/token</code> on the server. This browser stays logged in
          for 30 days.
        </p>
        <label className="visually-hidden" htmlFor="token">
          Access token
        </label>
        <input
          id="token"
          className="field"
          type="password"
          autoComplete="current-password"
          autoFocus
          required
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder="orla_…"
          spellCheck={false}
        />
        <button className="btn btn-primary" disabled={busy || !token.trim()}>
          {busy ? 'Logging in…' : 'Log in'}
        </button>
        {error && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
      </form>
    </main>
  );
}
