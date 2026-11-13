import '@fontsource/lilex/400.css';
import '@fontsource/lilex/700.css';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, Copy, Cross } from './Icons';

type Tone = 'error' | 'info' | 'ok';
interface Line {
  id: number;
  text: string;
  tone: Tone;
  at: Date;
}

const LEVEL: Record<Tone, string> = { error: 'ERR', ok: 'OK', info: 'INFO' };
// An error waits longer to be read; the log hides this long after its last line.
const LIFETIME: Record<Tone, number> = { error: 8000, ok: 4000, info: 4000 };

const clock = (d: Date) => d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

/** Writes a line to the log in the corner of the screen, from anywhere. */
export function toast(text: string, tone: Tone = 'error') {
  dispatchEvent(new CustomEvent<Omit<Line, 'id' | 'at'>>('orla-toast', { detail: { text, tone } }));
}

/**
 * Orla's notes, as a short log: time, level, message, newest last. It hides itself a while after
 * the last line; the bar underneath is that countdown, and hovering or focusing it pauses it.
 */
export function Toaster({ name = 'orla.log' }: { name?: string }) {
  const [lines, setLines] = useState<Line[]>([]);
  const timer = useRef<HTMLSpanElement>(null);
  const countdown = useRef<Animation | null>(null);

  useEffect(() => {
    let n = 0;
    const on = (e: Event) => {
      const d = (e as CustomEvent<Omit<Line, 'id' | 'at'>>).detail;
      const id = ++n;
      setLines((list) => [...list.filter((x) => x.text !== d.text), { id, at: new Date(), ...d }].slice(-3));
    };
    addEventListener('orla-toast', on);
    return () => removeEventListener('orla-toast', on);
  }, []);

  const clear = useCallback(() => setLines([]), []);
  const [copied, setCopied] = useState(false);
  const copy = () => {
    const text = lines.map((l) => `${clock(l.at)} ${LEVEL[l.tone]} ${l.text}`).join('\n');
    navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }, () => {});
  };
  const last = lines[lines.length - 1];

  // One animation is both the bar and the clock, so pausing one pauses the other. A new line
  // starts it over.
  useEffect(() => {
    const el = timer.current;
    if (!last || !el) return;
    const anim = el.animate([{ transform: 'scaleX(1)' }, { transform: 'scaleX(0)' }], { duration: LIFETIME[last.tone], fill: 'forwards' });
    countdown.current = anim;
    anim.finished.then(clear, () => {});
    return () => anim.cancel();
  }, [last, clear]);

  useEffect(() => {
    if (!last) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !document.querySelector('dialog[open]') && clear();
    addEventListener('keydown', onKey);
    return () => removeEventListener('keydown', onKey);
  }, [last, clear]);

  const hold = () => countdown.current?.pause();
  const release = (e: React.FocusEvent | React.MouseEvent) => {
    const box = e.currentTarget as HTMLElement;
    if (box.matches(':hover') || box.contains(document.activeElement)) return;
    countdown.current?.play();
  };

  return (
    <div className="toaster">
      {last && (
        <div
          className="toast"
          data-tone={last.tone}
          role="log"
          aria-live={last.tone === 'error' ? 'assertive' : 'polite'}
          aria-label="Notifications"
          onMouseEnter={hold}
          onMouseLeave={release}
          onFocus={hold}
          onBlur={release}
        >
          <div className="toast-head">
            <span>{name}</span>
            <span className="toast-acts">
              <button type="button" className="toast-clear" onClick={copy} aria-label={copied ? 'Copied' : 'Copy the log'}>
                {copied ? <Check /> : <Copy />}
                {copied ? 'Copied' : 'Copy'}
              </button>
              <button type="button" className="toast-clear" onClick={clear}>
                Clear
              </button>
              <button type="button" className="toast-clear toast-close" onClick={clear} aria-label="Close">
                <Cross />
              </button>
            </span>
          </div>
          <ol>
            {lines.map((l) => (
              <li key={l.id} data-latest={l === last} data-tone={l.tone}>
                <time>{clock(l.at)}</time>
                <span className="toast-level">{LEVEL[l.tone]}</span>
                <span className="toast-text">{l.text}</span>
              </li>
            ))}
          </ol>
          <span ref={timer} className="toast-timer" aria-hidden="true" />
        </div>
      )}
    </div>
  );
}
