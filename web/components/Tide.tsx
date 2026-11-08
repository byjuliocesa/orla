import { useEffect, useState } from 'react';
import { Sea } from './Sea';

const VERBS = ['Thinking', 'Reading the tide', 'Charting', 'Sounding the depth', 'Taking bearings', 'Riding the swell', 'Mending the nets', 'Waiting for the wave'];

/**
 * What Orla shows while Claude Code works: a small piece of the same sea as the login and the
 * prompt box, rolling, with a verb that changes now and then and how long it has been.
 */
export function Tide({ since, label }: { since: string; label?: string }) {
  const [now, setNow] = useState(() => Date.now());
  const [verb, setVerb] = useState(() => Math.floor(Math.random() * VERBS.length));

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    const v = setInterval(() => setVerb((i) => (i + 1 + Math.floor(Math.random() * (VERBS.length - 1))) % VERBS.length), 6000);
    return () => {
      clearInterval(t);
      clearInterval(v);
    };
  }, []);

  const secs = Math.max(0, Math.floor((now - Date.parse(since)) / 1000));
  const elapsed = secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m ${String(secs % 60).padStart(2, '0')}s`;

  return (
    <div className="tide" role="status" aria-live="polite">
      <span className="tide-sea" aria-hidden="true">
        <Sea state="idle" rest={0.42} swell={0.36} wavelength={0.22} />
      </span>
      <span className="tide-verb" key={verb}>
        {label ?? VERBS[verb]}…
      </span>
      <span className="tide-time">{elapsed}</span>
    </div>
  );
}
