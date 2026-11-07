import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import type { Session } from '@/lib/types';
import { Archive, Copy, Pencil, Pin, Trash } from './Icons';

export type SessionAction = 'rename' | 'pin' | 'copy' | 'archive' | 'remove';

type Item = { action: SessionAction; label: string; icon: ReactNode; danger?: boolean };

/** What a right click on a session offers, opened where the pointer is. */
export function SessionMenu({ session, x, y, onPick, onClose }: { session: Session; x: number; y: number; onPick: (a: SessionAction) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ x, y });

  const items: (Item | null)[] = [
    { action: 'rename', label: 'Rename', icon: <Pencil /> },
    { action: 'pin', label: session.pinned ? 'Unpin' : 'Pin to top', icon: <Pin /> },
    { action: 'copy', label: 'Copy folder path', icon: <Copy /> },
    { action: 'archive', label: session.archived ? 'Unarchive' : 'Archive', icon: <Archive /> },
    null,
    { action: 'remove', label: 'Remove…', icon: <Trash />, danger: true },
  ];

  // Kept inside the window: a click near the bottom or right edge opens the menu up or left.
  useLayoutEffect(() => {
    const m = ref.current;
    if (!m) return;
    const r = m.getBoundingClientRect();
    setPos({ x: Math.min(x, innerWidth - r.width - 8), y: Math.min(y, innerHeight - r.height - 8) });
    m.querySelector<HTMLElement>('[role=menuitem]')?.focus();
  }, [x, y]);

  useEffect(() => {
    const away = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && onClose();
    addEventListener('pointerdown', away, true);
    addEventListener('resize', onClose);
    addEventListener('blur', onClose);
    return () => {
      removeEventListener('pointerdown', away, true);
      removeEventListener('resize', onClose);
      removeEventListener('blur', onClose);
    };
  }, [onClose]);

  const onKey = (e: React.KeyboardEvent) => {
    const list = [...(ref.current?.querySelectorAll<HTMLElement>('[role=menuitem]') ?? [])];
    const i = list.indexOf(document.activeElement as HTMLElement);
    const go = (n: number) => list[(n + list.length) % list.length]?.focus();
    if (e.key === 'ArrowDown') go(i + 1);
    else if (e.key === 'ArrowUp') go(i - 1);
    else if (e.key === 'Home') go(0);
    else if (e.key === 'End') go(list.length - 1);
    else if (e.key === 'Escape' || e.key === 'Tab') onClose();
    else return;
    e.preventDefault();
  };

  return createPortal(
    <div ref={ref} className="menu" role="menu" aria-label="Session" style={{ left: pos.x, top: pos.y }} onKeyDown={onKey} onContextMenu={(e) => e.preventDefault()}>
      {items.map((it, i) =>
        it ? (
          <button key={it.action} type="button" role="menuitem" className="menu-item" data-action={it.action} data-danger={it.danger} style={{ animationDelay: `${i * 18}ms` }} onClick={() => onPick(it.action)}>
            {it.icon}
            {it.label}
          </button>
        ) : (
          <div key={i} className="menu-sep" role="separator" />
        ),
      )}
    </div>,
    document.body,
  );
}
