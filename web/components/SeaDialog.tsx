import { useEffect, useRef, useState, type ReactNode } from 'react';
import { SeaBackdrop } from './SeaBackdrop';

// How long the card and the sea take to leave; the dialog closes after them.
const LEAVE = 320;

/**
 * Orla's modal: a card over the sea, which rises as it opens and sinks as it closes. Escape and
 * a click beside the card close it, unless busy.
 */
export function SeaDialog({ open, onClose, busy = false, labelledBy, className, children }: { open: boolean; onClose: () => void; busy?: boolean; labelledBy: string; className?: string; children: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [shown, setShown] = useState(open);
  const [up, setUp] = useState(false);
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const d = ref.current;
    if (open) {
      setShown(true);
      setLeaving(false);
      return;
    }
    if (!d?.open) return;
    setLeaving(true);
    setUp(false);
    const t = setTimeout(() => {
      d.close();
      setShown(false);
      setLeaving(false);
    }, LEAVE);
    return () => clearTimeout(t);
  }, [open]);

  useEffect(() => {
    const d = ref.current;
    if (!shown || !open || !d || d.open) return;
    d.showModal();
    const raf = requestAnimationFrame(() => setUp(true));
    return () => cancelAnimationFrame(raf);
  }, [shown, open]);

  if (!shown) return null;
  return (
    <dialog
      ref={ref}
      className="ob sd"
      data-leaving={leaving || undefined}
      aria-labelledby={labelledBy}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
      onMouseDown={(e) => e.target === e.currentTarget && !busy && onClose()}
    >
      <SeaBackdrop up={up} />
      <div className={className ? `sd-card ${className}` : 'sd-card'}>{children}</div>
    </dialog>
  );
}
