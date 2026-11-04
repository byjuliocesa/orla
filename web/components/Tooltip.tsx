'use client';

import { cloneElement, useCallback, useEffect, useRef, useState, type ReactElement, type ReactNode, type SyntheticEvent } from 'react';
import { createPortal } from 'react-dom';

type Side = 'right' | 'top' | 'bottom';

// Once one tooltip has shown, its neighbours show at once while the pointer runs along a row.
let warmUntil = 0;

/**
 * A label shown beside its trigger on hover or keyboard focus. The trigger keeps its own
 * accessible name; this only shows it to the eye, so it must say nothing the name does not.
 */
export function Tooltip({ label, side = 'right', off = false, children }: { label: ReactNode; side?: Side; off?: boolean; children: ReactElement<Record<string, unknown>> }) {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  const timer = useRef<number | undefined>(undefined);

  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setAt((cur) => {
      if (cur) warmUntil = Date.now() + 400;
      return null;
    });
  }, []);

  const show = (el: HTMLElement) => {
    const r = el.getBoundingClientRect();
    const place = () =>
      setAt(side === 'right' ? { x: r.right + 10, y: r.top + r.height / 2 } : { x: r.left + r.width / 2, y: side === 'top' ? r.top - 8 : r.bottom + 8 });
    window.clearTimeout(timer.current);
    if (Date.now() < warmUntil) place();
    else timer.current = window.setTimeout(place, 350);
  };

  useEffect(() => {
    if (off) hide();
  }, [off, hide]);

  useEffect(() => {
    if (!at) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && hide();
    addEventListener('keydown', onKey);
    addEventListener('scroll', hide, true);
    return () => {
      removeEventListener('keydown', onKey);
      removeEventListener('scroll', hide, true);
    };
  }, [at, hide]);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  if (off) return children;
  const own = children.props as Record<string, ((e: SyntheticEvent<HTMLElement>) => void) | undefined>;
  const chain = (name: string, then: (e: SyntheticEvent<HTMLElement>) => void) => (e: SyntheticEvent<HTMLElement>) => {
    own[name]?.(e);
    then(e);
  };
  return (
    <>
      {cloneElement(children, {
        onMouseEnter: chain('onMouseEnter', (e) => show(e.currentTarget)),
        onMouseLeave: chain('onMouseLeave', hide),
        onFocus: chain('onFocus', (e) => e.currentTarget.matches(':focus-visible') && show(e.currentTarget)),
        onBlur: chain('onBlur', hide),
        onPointerDown: chain('onPointerDown', hide),
        onContextMenu: chain('onContextMenu', hide),
      })}
      {at &&
        createPortal(
          <div className="tip" data-side={side} style={{ left: at.x, top: at.y }} aria-hidden="true">
            {label}
          </div>,
          document.body,
        )}
    </>
  );
}
