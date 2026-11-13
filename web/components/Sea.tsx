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
  rest: restAt = 0.62,
  swell = 1,
  wavelength = 1,
  fps = 60,
  charSize,
  density = 2,
}: {
  state: SeaState;
  above?: React.RefObject<HTMLElement | null>;
  covered?: boolean;
  /** Where the water rests, as a share of the height from the top. */
  rest?: number;
  /** How high the waves run, as a share of the full sea's. */
  swell?: number;
  /** How long the waves are, as a share of the full sea's. */
  wavelength?: number;
  /** At most this many frames a second: a large, slow sea looks the same at fewer. */
  fps?: number;
  /** The characters' size in pixels; larger draws fewer of them. */
  charSize?: number;
  /** Pixels per CSS pixel at most: 1 halves the work on a sharp screen, for a sea in the back. */
  density?: number;
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
      const rest = above ? Math.max(formEnd + 64, h * restAt) : h * restAt;
      const s = stateRef.current;
      target =
        s === 'busy' ? formEnd + 26 : s === 'ok' ? COVERED : s === 'recede' ? h + 80 : s === 'error' ? rest + 28 : rest;
      if (Number.isNaN(level) || reduce) level = target;
      if (reduce) draw();
      else if (!raf && !document.hidden) {
        last = performance.now();
        raf = requestAnimationFrame(loop);
      }
    };
    aimRef.current = aim;

    // Read once per size, not per frame: computed style and fonts are slow to ask for.
    let crest = '212, 232, 239';
    let body = '108, 160, 182';
    let ground = '#091116';
    let cell = 14;
    const style = () => {
      // Colours come from the page (as "r, g, b") so the ground can change without touching this.
      const css = getComputedStyle(cv);
      crest = css.getPropertyValue('--sea-crest').trim() || '212, 232, 239';
      body = css.getPropertyValue('--sea-body').trim() || '108, 160, 182';
      ground = css.getPropertyValue('--bg').trim() || '#091116';
      cell = charSize ?? (w < 560 ? 12 : 14);
    };

    const draw = () => {
      ctx.clearRect(0, 0, w, h);
      const row = cell * 1.25;
      const step = cell * 0.62;
      const crestAt = (x: number) => level + (Math.sin((x * 0.011) / wavelength + t * 0.9) * 10 + Math.sin((x * 0.027) / wavelength - t * 1.3) * 5) * swell;

      ctx.fillStyle = ground;
      ctx.beginPath();
      ctx.moveTo(0, h);
      for (let x = 0; x < w + step; x += step) ctx.lineTo(x, crestAt(x) - row / 2);
      ctx.lineTo(w, h);
      ctx.closePath();
      ctx.fill();

      ctx.font = `500 ${cell}px Lilex, ui-monospace, monospace`;
      ctx.textBaseline = 'middle';
      const crestInk = `rgba(${crest}, 0.95)`;
      for (let x = 0; x < w + cell; x += step) {
        const top = crestAt(x);
        // Rows above the top edge are not drawn: a covered sea starts above the screen.
        for (let y = Math.max(top, top + Math.ceil((-row - top) / row) * row); y < h + row; y += row) {
          const depth = (y - top) / row;
          if (depth < 1) {
            ctx.fillStyle = crestInk;
            ctx.fillText('≈', x, y);
            continue;
          }
          const wave = Math.sin(x * 0.02 + y * 0.05 - t * 1.6) + Math.sin(x * 0.006 - t * 0.5);
          ctx.fillStyle = `rgba(${body}, ${Math.max(0.16, 0.62 - depth * 0.03 + wave * 0.07).toFixed(2)})`;
          ctx.fillText(CHARS[Math.max(0, Math.min(4, Math.round(1.6 + wave)))] ?? '~', x, y);
        }
      }
    };

    const size = () => {
      const r = cv.getBoundingClientRect();
      const dpr = Math.min(density, window.devicePixelRatio || 1);
      w = r.width;
      h = r.height;
      cv.width = w * dpr;
      cv.height = h * dpr;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      style();
      aim();
      draw();
    };

    const frame = 1000 / fps;
    const loop = (now: number) => {
      if (now - last < frame - 2) {
        raf = requestAnimationFrame(loop);
        return;
      }
      const dt = Math.min(0.1, (now - last) / 1000 || 0);
      last = now;
      t += dt;
      level += (target - level) * Math.min(1, dt * 2.4);
      draw();
      // Gone below the bottom and settled: nothing shows, so stop drawing until aimed again.
      if (stateRef.current === 'recede' && Math.abs(target - level) < 1) {
        raf = 0;
        return;
      }
      raf = requestAnimationFrame(loop);
    };

    let dead = false;
    const run = () => {
      cancelAnimationFrame(raf);
      raf = 0;
      if (dead || document.hidden) return;
      size();
      if (reduce) return;
      last = performance.now();
      raf = requestAnimationFrame(loop);
    };

    const ro = new ResizeObserver(size);
    ro.observe(cv);
    document.addEventListener('visibilitychange', run);
    document.fonts?.ready.then(() => !dead && run());
    run();
    return () => {
      dead = true;
      cancelAnimationFrame(raf);
      ro.disconnect();
      document.removeEventListener('visibilitychange', run);
      aimRef.current = () => {};
    };
  }, [above, covered, restAt, swell, wavelength, fps, charSize, density]);

  return <canvas ref={canvas} className="sea" aria-hidden="true" />;
}
