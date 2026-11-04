'use client';

import { useEffect, useRef, type ReactNode } from 'react';

/** A modal question with one way forward and one way back. Escape and Cancel both close it. */
export function ConfirmDialog({
  open,
  title,
  children,
  confirm,
  busy = false,
  error,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  confirm: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      className="confirm"
      aria-labelledby="confirm-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
      onClick={(e) => e.target === e.currentTarget && !busy && onClose()}
    >
      {open && (
        <div className="confirm-box">
          <h2 id="confirm-title">{title}</h2>
          <div className="confirm-body">{children}</div>
          {error && (
            <p className="confirm-error" role="alert">
              {error}
            </p>
          )}
          <div className="confirm-actions">
            {/* Focus starts on the way back, so Enter alone never does the irreversible thing. */}
            <button type="button" className="btn" onClick={onClose} disabled={busy} autoFocus>
              Cancel
            </button>
            <button type="button" className="btn btn-danger" onClick={onConfirm} disabled={busy}>
              {busy ? `${confirm}…` : confirm}
            </button>
          </div>
        </div>
      )}
    </dialog>
  );
}
