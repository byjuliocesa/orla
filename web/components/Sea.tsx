'use client';

import { useEffect, useRef } from 'react';

export type SeaState = 'idle' | 'busy' | 'error' | 'ok' | 'recede';

const CHARS = ['.', '·', '-', '~', '≈'];
// Above the top edge by more than the waves' reach, so nothing shows through.
const COVERED = -60;

// A sea drawn in Lilex characters. On the login its waterline is the form's state: it rests below
// the form, rises to it while the token is checked, and covers the screen on success. The panel
// then opens under a covered sea that recedes off the bottom. Water hides what is beneath it.
export function Sea({
  state,
  above,
  covered = false,
}: {
  state: SeaState;
  above?: React.RefObject<HTMLElement | null>;
  covered?: boolean;
}) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const stateRef = useRef(state);
  const aimRef = useRef<() => void>(() => {});

  useEffect(() => {
    stateRef.current = state;
    aimRef.current();
  }, [state]);

  useEffect(() => {
    const cv = canvas.current;
    const ctx = cv?.getContext('2d');
    if (!cv || !ctx) return;
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    let w = 0;
    let h = 0;
    let level = covered ? COVERED : Number.NaN;
    let target = 0;
    let t = 0;
    let raf = 0;
    let last = 0;

    const aim = () => {
      const top = cv.getBoundingClientRect().top;
      const formEnd = (above?.current?.getBoundingClientRect().bottom ?? 0) - top;
      const rest = Math.max(formEnd + 64, h * 0.62);
      const s = stateRef.current;
      target =
        s === 'busy' ? formEnd + 26 : s === 'ok' ? COVERED : s === 'recede' ? h + 80 : s === 'error' ? rest + 28 : rest;
      if (Number.isNaN(level) || reduce) level = target;
      if (reduce) draw();
    };
    aimRef.current = aim;

    const draw = () => {
      ctx.clearRect(0, 0, w, h);
      // Colours come from the page (as "r, g, b") so the ground can change without touching this.
      const css = getComputedStyle(cv);
      const crest = css.getPropertyValue('--sea-crest').trim() || '212, 232, 239';
      const body = css.getPropertyValue('--sea-body').trim() || '108, 160, 182';
      const cell = w < 560 ? 12 : 14;
      const row = cell * 1.25;
      const step = cell * 0.62;
      const crestAt = (x: number) => level + Math.sin(x * 0.011 + t * 0.9) * 10 + Math.sin(x * 0.027 - t * 1.3) * 5;

      ctx.fillStyle = css.getPropertyValue('--bg').trim() || '#091116';
      ctx.beginPath();
      ctx.moveTo(0, h);
      for (let x = 0; x < w + step; x += step) ctx.lineTo(x, crestAt(x) - row / 2);
      ctx.lineTo(w, h);
      ctx.closePath();
      ctx.fill();

      ctx.font = `500 ${cell}px Lilex, ui-monospace, monospace`;
      ctx.textBaseline = 'middle';
      for (let x = 0; x < w + cell; x += step) {
        const top = crestAt(x);
        for (let y = top; y < h + row; y += row) {
          const depth = (y - top) / row;
          const wave = Math.sin(x * 0.02 + y * 0.05 - t * 1.6) + Math.sin(x * 0.006 - t * 0.5);
          if (depth < 1) {
            ctx.fillStyle = `rgba(${crest}, 0.95)`;
            ctx.fillText('≈', x, y);
          } else {
            const alpha = Math.max(0.16, 0.62 - depth * 0.03 + wave * 0.07);
            ctx.fillStyle = `rgba(${body}, ${alpha})`;
            ctx.fillText(CHARS[Math.max(0, Math.min(4, Math.round(1.6 + wave)))] ?? '~', x, y);
          }
        }
      }
    };

    const size = () => {
      const r = cv.getBoundingClientRect();
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      w = r.width;
      h = r.height;
      cv.width = w * dpr;
      cv.height = h * dpr;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      aim();
      draw();
    };

    const loop = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000 || 0);
      last = now;
      t += dt;
      level += (target - level) * Math.min(1, dt * 2.4);
      draw();
      raf = requestAnimationFrame(loop);
    };

    const run = () => {
      cancelAnimationFrame(raf);
      if (document.hidden) return;
      size();
      if (reduce) return;
      last = performance.now();
      raf = requestAnimationFrame(loop);
    };

    const ro = new ResizeObserver(size);
    ro.observe(cv);
    document.addEventListener('visibilitychange', run);
    document.fonts?.ready.then(run);
    run();
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
      document.removeEventListener('visibilitychange', run);
      aimRef.current = () => {};
    };
  }, [above, covered]);

  return <canvas ref={canvas} className="sea" aria-hidden="true" />;
}
