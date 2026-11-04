'use client';

import '@fontsource/lilex/500.css';
import { useEffect, useState } from 'react';
import { Sea, type SeaState } from './Sea';

// Set by the login just before it hands over, so only a fresh login gets the arrival.
export const ARRIVAL_KEY = 'orla.arrival';

const HOLD = 220;
const RECEDE = 1500;

function arriving(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return sessionStorage.getItem(ARRIVAL_KEY) === '1';
  } catch {
    return false;
  }
}

// After a login the panel opens under the sea that covered the login screen; the water recedes
// while the panel assembles, and data-arriving on <html> times that assembly in CSS.
export function Arrival() {
  const [on, setOn] = useState(arriving);
  const [state, setState] = useState<SeaState>('ok');

  useEffect(() => {
    if (!on) return;
    try {
      sessionStorage.removeItem(ARRIVAL_KEY);
    } catch {}
    const root = document.documentElement;
    root.dataset.arriving = '';
    const recede = setTimeout(() => setState('recede'), HOLD);
    const done = setTimeout(() => {
      delete root.dataset.arriving;
      setOn(false);
    }, HOLD + RECEDE);
    return () => {
      clearTimeout(recede);
      clearTimeout(done);
      delete root.dataset.arriving;
    };
  }, [on]);

  if (!on) return null;
  return (
    <div className="arrival" aria-hidden="true">
      <Sea state={state} covered />
    </div>
  );
}
