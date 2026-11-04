import { useId } from 'react';
import { WORDMARK_GLYPHS } from './wordmark';

// useId's characters are not valid inside url(#…).
const useClipId = () => 'logo' + useId().replace(/[^a-zA-Z0-9_-]/g, '');

// Two wavelengths more than the mark shows, one each side, so the water can travel one wavelength
// (48) and loop; inside the circle it draws the same line as before.
const WAVE = 'M-48 35 C-40 29 -32 29 -24 35 S-8 41 0 35 S16 29 24 35 S40 41 48 35 S64 29 72 35 S88 41 96 35 S112 29 120 35 V64 H-48 Z';

// The ring takes currentColor so the mark follows the text it sits beside.
function MarkShapes({ clip }: { clip: string }) {
  return (
    <>
      <defs>
        <clipPath id={clip}>
          <circle cx="32" cy="32" r="22" />
        </clipPath>
      </defs>
      {/* The clip sits on the group, so moving the water does not move the circle that holds it. */}
      <g clipPath={`url(#${clip})`}>
        <path className="logo-wave" fill="var(--sea)" d={WAVE} />
      </g>
      <circle cx="32" cy="32" r="22" fill="none" stroke="currentColor" strokeWidth="5" />
    </>
  );
}

export function Mark({ size = 20 }: { size?: number }) {
  const clip = useClipId();
  return (
    <svg className="logo-mark" viewBox="0 0 64 64" width={size} height={size} aria-hidden="true">
      <MarkShapes clip={clip} />
    </svg>
  );
}

// Wordmark scale: 0.05 puts the x-height at 26 of the mark's 64 units.
const LOCKUP_WIDTH = 76 + 2181 * 0.05 + 4;

export function Lockup({ height = 28 }: { height?: number }) {
  const clip = useClipId();
  return (
    <svg
      className="logo-lockup"
      viewBox={`0 0 ${LOCKUP_WIDTH} 64`}
      height={height}
      width={(height * LOCKUP_WIDTH) / 64}
      role="img"
      aria-label="Orla"
    >
      <MarkShapes clip={clip} />
      <g transform="translate(76 46) scale(0.05)" fill="currentColor">
        {WORDMARK_GLYPHS.map((g) => (
          <path key={g.x} transform={`translate(${g.x} 0)`} d={g.d} />
        ))}
      </g>
    </svg>
  );
}
