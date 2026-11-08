import { useEffect, useRef, useState, type ReactNode } from 'react';

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
  danger = true,
}: {
  /** The way forward undoes something; false makes it the plain blue action. */
  danger?: boolean;
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
            <button type="button" className={danger ? 'btn btn-danger' : 'btn btn-primary'} onClick={onConfirm} disabled={busy}>
              {busy ? `${confirm}…` : confirm}
            </button>
          </div>
        </div>
      )}
    </dialog>
  );
}

interface Ask {
  title: string;
  body: string;
  confirm: string;
  danger?: boolean;
}

/** Asks a yes-or-no question in Orla's own dialog, from anywhere; resolves with the answer. */
export function ask(q: Ask): Promise<boolean> {
  return new Promise((resolve) => dispatchEvent(new CustomEvent('orla-ask', { detail: { ...q, resolve } })));
}

/** Where ask() shows its dialog; mounted once by the panel. */
export function AskHost() {
  const [q, setQ] = useState<(Ask & { resolve: (ok: boolean) => void }) | null>(null);
  useEffect(() => {
    const on = (e: Event) => setQ((e as CustomEvent<Ask & { resolve: (ok: boolean) => void }>).detail);
    addEventListener('orla-ask', on);
    return () => removeEventListener('orla-ask', on);
  }, []);
  const answer = (ok: boolean) => {
    q?.resolve(ok);
    setQ(null);
  };
  return (
    <ConfirmDialog open={q !== null} title={q?.title ?? ''} confirm={q?.confirm ?? 'OK'} danger={q?.danger ?? true} onConfirm={() => answer(true)} onClose={() => answer(false)}>
      {q?.body}
    </ConfirmDialog>
  );
}
