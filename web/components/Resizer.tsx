import { useCallback, useState, type CSSProperties } from 'react';

/** A width the owner sets by dragging, kept between visits. */
export function useWidth(key: string, initial: number, min: number, max: number): [number, (w: number) => void, () => void] {
  const clamp = (w: number) => Math.round(Math.min(max, Math.max(min, w)));
  const [width, setWidth] = useState(() => {
    try {
      const v = Number(localStorage.getItem(key));
      return v ? clamp(v) : initial;
    } catch {
      return initial;
    }
  });
  const set = useCallback(
    (w: number) => {
      const next = clamp(w);
      setWidth(next);
      try {
        localStorage.setItem(key, String(next));
      } catch {}
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [key, min, max],
  );
  const reset = useCallback(() => {
    setWidth(initial);
    try {
      localStorage.removeItem(key);
    } catch {}
  }, [key, initial]);
  return [width, set, reset];
}

/**
 * The handle on the edge of a column: drag it, use the arrow keys on it, or double-click it to
 * go back to the default. `edge` says which side of the window the column is on.
 */
export function Resizer({
  edge,
  width,
  setWidth,
  reset,
  label,
  style,
  onDrag,
}: {
  edge: 'left' | 'right';
  width: number;
  setWidth: (w: number) => void;
  reset: () => void;
  label: string;
  style?: CSSProperties;
  onDrag?: (dragging: boolean) => void;
}) {
  const [dragging, setDragging] = useState(false);
  const start = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const x0 = e.clientX;
    const w0 = width;
    setDragging(true);
    onDrag?.(true);
    document.body.dataset.resizing = '';
    const move = (m: PointerEvent) => setWidth(edge === 'left' ? w0 + (m.clientX - x0) : w0 - (m.clientX - x0));
    const up = () => {
      setDragging(false);
      onDrag?.(false);
      delete document.body.dataset.resizing;
      removeEventListener('pointermove', move);
      removeEventListener('pointerup', up);
    };
    addEventListener('pointermove', move);
    addEventListener('pointerup', up);
  };
  return (
    <div
      className="resizer"
      data-edge={edge}
      data-dragging={dragging || undefined}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={width}
      tabIndex={0}
      style={style}
      onPointerDown={start}
      onDoubleClick={reset}
      onKeyDown={(e) => {
        const step = e.shiftKey ? 40 : 10;
        const grow = edge === 'left' ? 'ArrowRight' : 'ArrowLeft';
        const shrink = edge === 'left' ? 'ArrowLeft' : 'ArrowRight';
        if (e.key === grow) setWidth(width + step);
        else if (e.key === shrink) setWidth(width - step);
        else return;
        e.preventDefault();
      }}
    />
  );
}
