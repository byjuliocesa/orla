import { useEffect, useRef, useState } from 'react';

/**
 * Text that types itself out. On mount it shows at once, unless `fromStart` asks for the typing;
 * after that, every new value is typed. Reduced motion shows it at once.
 */
export function Typed({ text, fromStart = false, speed = 38, className }: { text: string; fromStart?: boolean; speed?: number; className?: string }) {
  const [shown, setShown] = useState(fromStart ? '' : text);
  // The value on mount shows as it is; typing starts with the first change (StrictMode-safe).
  const initial = useRef(fromStart ? null : text);

  useEffect(() => {
    if (text === initial.current) return;
    initial.current = null;
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setShown(text);
      return;
    }
    const chars = Array.from(text);
    let i = 0;
    setShown('');
    const t = setInterval(() => {
      i += 1;
      setShown(chars.slice(0, i).join(''));
      if (i >= chars.length) clearInterval(t);
    }, speed);
    return () => clearInterval(t);
  }, [text, speed]);

  const typing = shown !== text;
  return (
    <span className={className} data-typing={typing || undefined}>
      <span className="visually-hidden">{text}</span>
      <span aria-hidden="true">{shown}</span>
    </span>
  );
}
