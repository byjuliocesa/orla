import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, uploadFile, type Upload } from '@/lib/api';
import { ChevronRight, Cross, FileIcon, Spinner, VideoIcon } from './Icons';
import { Tooltip } from './Tooltip';

/** One attached file: still going up, up, or failed. */
export interface Attachment {
  key: string;
  name: string;
  type: string;
  size: number;
  progress: number;
  upload?: Upload;
  error?: string;
}

function restore(store: string): Attachment[] {
  try {
    const saved = JSON.parse(localStorage.getItem(store) ?? '[]') as Upload[];
    return saved.map((u) => ({ key: u.id, name: u.name, type: u.type, size: u.size, progress: 1, upload: u }));
  } catch {
    return [];
  }
}

/**
 * Files attached to a prompt. Each goes up as soon as it is added; those that made it are kept
 * with the draft, so a reload keeps them too.
 */
export function useAttachments(store: string) {
  const [items, setItems] = useState<Attachment[]>(() => restore(store));
  const aborts = useRef(new Map<string, () => void>());

  useEffect(() => {
    try {
      const done = items.flatMap((a) => (a.upload ? [a.upload] : []));
      if (done.length) localStorage.setItem(store, JSON.stringify(done));
      else localStorage.removeItem(store);
    } catch {}
  }, [items, store]);

  useEffect(() => {
    const pending = aborts.current;
    return () => pending.forEach((abort) => abort());
  }, []);

  const patch = (key: string, change: Partial<Attachment>) => setItems((list) => list.map((a) => (a.key === key ? { ...a, ...change } : a)));

  const add = useCallback((files: Iterable<File>) => {
    for (const file of files) {
      const key = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const name = file.name || (file.type.startsWith('image/') ? `pasted.${file.type.split('/')[1] ?? 'png'}` : 'file');
      setItems((list) => [...list, { key, name, type: file.type, size: file.size, progress: 0 }]);
      const { done, abort } = uploadFile(new File([file], name, { type: file.type }), (progress) => patch(key, { progress }));
      aborts.current.set(key, abort);
      done
        .then((upload) => patch(key, { upload, progress: 1, type: upload.type || file.type }))
        .catch((err: unknown) => {
          if (err instanceof ApiError && err.message === 'aborted') return;
          patch(key, { error: err instanceof Error ? err.message : 'The upload failed.' });
        })
        .finally(() => aborts.current.delete(key));
    }
  }, []);

  const remove = useCallback((key: string) => {
    aborts.current.get(key)?.();
    setItems((list) => list.filter((a) => a.key !== key));
  }, []);

  const clear = useCallback(() => setItems([]), []);

  const uploading = items.some((a) => !a.upload && !a.error);
  const failed = items.some((a) => a.error);
  const ids = items.flatMap((a) => (a.upload ? [a.upload.id] : []));
  return { items, add, remove, clear, uploading, failed, ids };
}

export function size(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  return `${(n / 1024 / 1024).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}

export function ext(name: string) {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(i + 1).toUpperCase().slice(0, 5) : 'FILE';
}

const TEXT_EXT = /\.(txt|md|markdown|json|ya?ml|toml|ini|env|csv|tsv|log|xml|html?|css|scss|js|jsx|ts|tsx|mjs|cjs|go|rs|py|rb|php|java|kt|swift|c|h|cc|cpp|hpp|cs|sh|bash|zsh|fish|sql|graphql|proto|lua|dart|vue|svelte)$/i;
const BY_EXT: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', avif: 'image/avif', svg: 'image/svg+xml',
  mp4: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', m4v: 'video/mp4',
  mp3: 'audio/mpeg', wav: 'audio/wav', ogg: 'audio/ogg', m4a: 'audio/mp4', flac: 'audio/flac',
  pdf: 'application/pdf',
};
/** A type from a file's name, for files known only by their path. */
export const guessType = (name: string) => BY_EXT[name.split('.').pop()?.toLowerCase() ?? ''] ?? '';

export const kindOf = (a: { name: string; type: string }) =>
  a.type.startsWith('image/')
    ? 'image'
    : a.type.startsWith('video/')
      ? 'video'
      : a.type.startsWith('audio/')
        ? 'audio'
        : a.type.startsWith('text/') || /json|xml|javascript|yaml|toml/.test(a.type) || TEXT_EXT.test(a.name)
          ? 'text'
          : 'file';

/** The attached files above the prompt: a picture for images, a card for the rest. A click opens it. */
export function AttachmentTray({ items, remove }: { items: Attachment[]; remove: (key: string) => void }) {
  const [open, setOpen] = useState<string | null>(null);
  if (items.length === 0) return null;
  const done = items.filter((a) => a.upload);
  const current = done.find((a) => a.key === open);
  return (
    <>
      <ul className="att-tray" aria-label="Attached files">
        {items.map((a) => {
          const kind = kindOf(a);
          const image = kind === 'image';
          return (
            <li key={a.key} className="att" data-kind={image ? 'image' : 'file'} data-state={a.error ? 'error' : a.upload ? 'done' : 'up'}>
              <Tooltip label={a.error ? `${a.name}: ${a.error}` : a.name} side="top">
                <button type="button" className="att-open" onClick={() => a.upload && setOpen(a.key)} disabled={!a.upload} aria-label={`Open ${a.name}`}>
                  {image && a.upload ? (
                    <img src={`/v1/uploads/${a.upload.id}`} alt="" />
                  ) : (
                    <span className="att-card">
                      <span className="att-icon">{kind === 'video' ? <VideoIcon /> : <FileIcon />}</span>
                      <span className="att-text">
                        <span className="att-name">{a.name}</span>
                        <span className="att-meta">{a.error ? 'Failed' : `${ext(a.name)} · ${size(a.size)}`}</span>
                      </span>
                    </span>
                  )}
                  {!a.upload && !a.error && (
                    <span className="att-progress" aria-label={`Uploading ${Math.round(a.progress * 100)}%`}>
                      <Spinner />
                      <span className="att-bar" style={{ transform: `scaleX(${a.progress})` }} />
                    </span>
                  )}
                </button>
              </Tooltip>
              <button type="button" className="att-remove" onClick={() => remove(a.key)} aria-label={`Remove ${a.name}`}>
                <Cross />
              </button>
            </li>
          );
        })}
      </ul>
      {current && <AttachmentPreview items={done} current={current} go={setOpen} onClose={() => setOpen(null)} />}
    </>
  );
}

/** One attached file, large: the picture, the player, the text, or what it is and a download. */
export function AttachmentPreview({ items, current, go, onClose }: { items: Attachment[]; current: Attachment; go: (key: string) => void; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [text, setText] = useState<string | null>(null);
  const u = current.upload!;
  const src = `/v1/uploads/${u.id}`;
  const kind = kindOf(current);
  const i = items.indexOf(current);

  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);

  useEffect(() => {
    setText(null);
    if (kind !== 'text') return;
    if (u.size > 512 * 1024) return setText('');
    let stale = false;
    fetch(src, { credentials: 'same-origin' })
      .then((r) => r.text())
      .then((t) => !stale && setText(t))
      .catch(() => !stale && setText(''));
    return () => {
      stale = true;
    };
  }, [src, kind, u.size]);

  const step = (d: number) => items.length > 1 && go(items[(i + d + items.length) % items.length]!.key);

  return (
    <dialog
      ref={ref}
      className="att-view"
      aria-label={current.name}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => e.target === e.currentTarget && onClose()}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft') step(-1);
        if (e.key === 'ArrowRight') step(1);
      }}
    >
      <header className="att-view-head">
        <span className="att-view-name">{current.name}</span>
        <span className="att-view-meta">
          {ext(current.name)}
          {current.size > 0 && ` · ${size(current.size)}`}
          {items.length > 1 && ` · ${i + 1} of ${items.length}`}
        </span>
        <span className="nc-grow" />
        <a className="btn btn-sm" href={src} download={current.name}>
          Download
        </a>
        <button type="button" className="icon-btn" onClick={onClose} aria-label="Close" autoFocus>
          <Cross />
        </button>
      </header>
      <div className="att-view-body" data-kind={kind} key={u.id}>
        {kind === 'image' && <img src={src} alt={current.name} />}
        {kind === 'video' && <video src={src} controls autoPlay playsInline />}
        {kind === 'audio' && <audio src={src} controls autoPlay />}
        {kind === 'text' &&
          (text === null ? (
            <p className="att-view-note">Loading…</p>
          ) : text === '' ? (
            <p className="att-view-note">Too large to show here. Download it to read it.</p>
          ) : (
            <pre>{text}</pre>
          ))}
        {kind === 'file' && (
          <div className="att-view-file">
            <span className="att-icon">
              <FileIcon />
            </span>
            <p>No preview for this kind of file. The agent can still read it.</p>
          </div>
        )}
      </div>
      {items.length > 1 && (
        <>
          <button type="button" className="att-view-step" data-dir="prev" onClick={() => step(-1)} aria-label="Previous file">
            <ChevronRight />
          </button>
          <button type="button" className="att-view-step" data-dir="next" onClick={() => step(1)} aria-label="Next file">
            <ChevronRight />
          </button>
        </>
      )}
    </dialog>
  );
}
