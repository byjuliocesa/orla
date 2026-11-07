import { useState, type FormEvent } from 'react';
import { api, ApiError } from '@/lib/api';

/**
 * Allow and Deny for one pending request. Deny opens a field for a note, which the agent reads
 * as the owner's reason.
 */
export function AskActions({ session, approval, onAnswered }: { session: string; approval: string; onAnswered?: () => void }) {
  const [denying, setDenying] = useState(false);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const answer = async (allow: boolean, message?: string) => {
    setBusy(true);
    setError('');
    try {
      await api.resolve(session, approval, allow, message);
      onAnswered?.();
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 404
          ? 'Already answered, in the terminal or from another device.'
          : 'The answer did not reach Orla. Try again.',
      );
    } finally {
      setBusy(false);
    }
  };

  if (denying) {
    const submit = (e: FormEvent) => {
      e.preventDefault();
      void answer(false, note.trim());
    };
    return (
      <form className="deny-form" onSubmit={submit}>
        <label className="visually-hidden" htmlFor={`note-${approval}`}>
          Note for the agent
        </label>
        <input
          id={`note-${approval}`}
          className="field"
          autoFocus
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Why not, or what to do instead (optional)"
          maxLength={2000}
          onKeyDown={(e) => e.key === 'Escape' && setDenying(false)}
        />
        <button className="btn btn-danger" disabled={busy}>
          Deny
        </button>
        <button type="button" className="btn" onClick={() => setDenying(false)} disabled={busy}>
          Back
        </button>
        {error && (
          <span role="alert" className="form-error">
            {error}
          </span>
        )}
      </form>
    );
  }

  return (
    <div className="ask-actions">
      <button className="btn btn-primary btn-sm" onClick={() => void answer(true)} disabled={busy}>
        Allow
      </button>
      <button className="btn btn-sm" onClick={() => setDenying(true)} disabled={busy}>
        Deny…
      </button>
      {error && (
        <span role="alert" className="form-error" style={{ marginTop: 0 }}>
          {error}
        </span>
      )}
    </div>
  );
}

export function timeLeft(expiresAt: string | undefined, now: number): string {
  if (!expiresAt) return '';
  const ms = Date.parse(expiresAt) - now;
  if (ms <= 0) return 'expiring';
  // Rounded: a request seen a moment after it was made still reads "10 min left".
  const m = Math.round(ms / 60_000);
  return m <= 1 ? 'under a minute left' : `${m} min left`;
}
