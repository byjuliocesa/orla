import '@/styles/sync.css';
import { useEffect, useRef, useState } from 'react';
import { Sea, type SeaState } from './Sea';
import { Connect } from './SyncView';

// After a login the arrival's water recedes for about this long; the dialog waits for it.
const AFTER_ARRIVAL = 1750;
const SETTLE = 250;

/**
 * The first visit: a dialog to bring the library from its repository, or to start without one.
 * It rises with a sea of its own once the arrival is over, and shows until the owner connects or
 * skips; Library › Sync connects later.
 */
export function Onboarding({ done }: { done: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [sea, setSea] = useState<SeaState>('recede');
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const wait = document.documentElement.hasAttribute('data-arriving') ? AFTER_ARRIVAL : SETTLE;
    const open = setTimeout(() => {
      ref.current?.showModal();
      setSea('idle'); // the water rises from below the screen to its rest
    }, wait);
    return () => clearTimeout(open);
  }, []);

  const close = () => {
    if (leaving) return;
    setLeaving(true);
    setSea('recede');
    setTimeout(() => {
      ref.current?.close();
      done();
    }, 520);
  };

  return (
    <dialog
      ref={ref}
      className="ob"
      data-leaving={leaving || undefined}
      aria-labelledby="ob-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <div className="ob-sea" aria-hidden="true">
        <Sea state={sea} rest={0.66} swell={0.8} wavelength={0.8} />
      </div>
      <div className="ob-card">
        <Connect connected={close} title="Bring your library" lead="Paste the git repository with your skills, flows and sessions. An empty one starts a new library." titleId="ob-title">
          <button type="button" className="ob-skip" onClick={close}>
            Not now
          </button>
        </Connect>
      </div>
    </dialog>
  );
}
