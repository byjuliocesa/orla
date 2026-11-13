import { useEffect, useRef } from 'react';

const CHARS = ['.', '·', '-', '~', '≈'];
// One period of every wave: the tile repeats seamlessly, so sliding it by this much loops.
const PERIOD = 960;
const COLS = 86; // characters per period
const CELL = 17;

/**
 * The Orla sea as a backdrop: drawn once, then only moved. A canvas redrawn every frame costs a
 * full core or two in some browsers at full screen; sliding a drawn image is the compositor's
 * work, nearly free. It rises when `up` turns true and sinks when it turns false.
 */
export function SeaBackdrop({ up }: { up: boolean }) {
  const canvas = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const cv = canvas.current;
    const ctx = cv?.getContext('2d');
    if (!cv || !ctx) return;
    const draw = () => {
      const w = window.innerWidth + PERIOD;
      const h = window.innerHeight;
      cv.width = w;
      cv.height = h;
      cv.style.width = `${w}px`;
      cv.style.height = `${h}px`;
      const css = getComputedStyle(cv);
      const crest = css.getPropertyValue('--sea-crest').trim() || '212, 232, 239';
      const body = css.getPropertyValue('--sea-body').trim() || '108, 160, 182';
      const k = (2 * Math.PI) / PERIOD;
      const step = PERIOD / COLS;
      const row = CELL * 1.25;
      const level = h * 0.03;
      const crestAt = (x: number) => level + (Math.sin(x * k) * 10 + Math.sin(x * k * 3) * 5) * 0.8;
      ctx.clearRect(0, 0, w, h);
      ctx.font = `500 ${CELL}px Lilex, ui-monospace, monospace`;
      ctx.textBaseline = 'middle';
      for (let i = 0; i * step < w + CELL; i++) {
        const x = i * step;
        const top = crestAt(x);
        ctx.fillStyle = `rgba(${crest}, 0.95)`;
        ctx.fillText('≈', x, top);
        for (let y = top + row; y < h + row; y += row) {
          const depth = (y - top) / row;
          const wave = Math.sin(x * k * 5 + y * 0.05) + Math.sin(x * k);
          ctx.fillStyle = `rgba(${body}, ${Math.max(0.16, 0.62 - depth * 0.03 + wave * 0.07).toFixed(2)})`;
          ctx.fillText(CHARS[Math.max(0, Math.min(4, Math.round(1.6 + wave)))] ?? '~', x, y);
        }
      }
    };
    let dead = false;
    let t = 0;
    const later = () => {
      clearTimeout(t);
      t = window.setTimeout(() => !dead && draw(), 150);
    };
    document.fonts?.ready.then(() => !dead && draw());
    draw();
    addEventListener('resize', later);
    return () => {
      dead = true;
      clearTimeout(t);
      removeEventListener('resize', later);
    };
  }, []);

  return (
    <div className="sb" data-up={up || undefined} aria-hidden="true">
      <canvas ref={canvas} className="sb-tile" />
    </div>
  );
}
