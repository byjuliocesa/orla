'use client';

import { useEffect, useRef, useState } from 'react';

export type LoginError = { id: number; kind: 'token' | 'down'; at: Date };

const TEXT = {
  token: 'That is not this server’s token. Copy it again from the token file.',
  down: 'Orla did not answer. Check that it is running and try again.',
};

const LIFETIME = 8000;

const time = (d: Date) => d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

// Failed attempts as lines of a log, newest last. It hides itself 8 s after the last error; the bar
// underneath is that countdown, and hovering or focusing the log pauses it where it is.
export function LoginToast({ error, onDismiss }: { error: LoginError | null; onDismiss: () => void }) {
  const [lines, setLines] = useState<LoginError[]>([]);
  const timer = useRef<HTMLSpanElement>(null);
  const countdown = useRef<Animation | null>(null);
  const seen = useRef(0);

  useEffect(() => {
    if (!error) return setLines([]);
    if (error.id === seen.current) return;
    seen.current = error.id;
    setLines((l) => [...l, error].slice(-3));
  }, [error]);

  // One animation is both the bar and the clock, so pausing one pauses the other.
  useEffect(() => {
    const el = timer.current;
    if (!error || !el) return;
    const anim = el.animate([{ transform: 'scaleX(1)' }, { transform: 'scaleX(0)' }], {
      duration: LIFETIME,
      fill: 'forwards',
    });
    countdown.current = anim;
    anim.finished.then(onDismiss, () => {});
    return () => anim.cancel();
  }, [error, lines.length, onDismiss]);

  const hold = () => countdown.current?.pause();
  const release = (e: React.FocusEvent | React.MouseEvent) => {
    const box = e.currentTarget as HTMLElement;
    if (box.matches(':hover') || box.contains(document.activeElement)) return;
    countdown.current?.play();
  };

  if (!lines.length || !error) return null;
  return (
    <div
      className="toast"
      role="log"
      aria-live="assertive"
      aria-label="Login errors"
      onMouseEnter={hold}
      onMouseLeave={release}
      onFocus={hold}
      onBlur={release}
    >
      <div className="toast-head">
        <span>login.log</span>
        <button type="button" className="toast-clear" onClick={onDismiss}>
          Clear
        </button>
      </div>
      <ol>
        {lines.map((l, i) => (
          <li key={l.id} data-latest={i === lines.length - 1}>
            <time>{time(l.at)}</time>
            <span className="toast-level">ERR</span>
            <span className="toast-text">{TEXT[l.kind]}</span>
          </li>
        ))}
      </ol>
      <span ref={timer} className="toast-timer" aria-hidden="true" />
    </div>
  );
}
