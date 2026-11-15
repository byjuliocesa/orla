import '@/styles/sync.css';
import { useEffect, useRef, useState } from 'react';
import { AgentStep } from './AgentStep';
import { SeaBackdrop } from './SeaBackdrop';
import { Connect } from './SyncView';
import { Toaster } from './Toast';

// After a login the arrival's water recedes for about this long; the dialog waits for it.
const AFTER_ARRIVAL = 1750;
const SETTLE = 250;

/**
 * The first visit: the agent, ready on this machine, then the library from its repository.
 * It rises with a sea of its own once the arrival is over, and shows until the owner connects or
 * skips; Library › Sync connects later.
 */
export function Onboarding({ done, library }: { done: () => void; library: boolean }) {
  const ref = useRef<HTMLDialogElement>(null);
  // The agent comes first: without it nothing in Orla runs.
  const [step, setStep] = useState<'agent' | 'library'>('agent');
  const [sea, setSea] = useState(false);
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const wait = document.documentElement.hasAttribute('data-arriving') ? AFTER_ARRIVAL : SETTLE;
    const open = setTimeout(() => {
      ref.current?.showModal();
      // Focus the card, not its first button: a ring on a button nobody chose reads as a choice.
      ref.current?.querySelector<HTMLElement>('.ob-card')?.focus();
      setSea(true); // the water rises from below the screen
    }, wait);
    return () => clearTimeout(open);
  }, []);

  const close = () => {
    if (leaving) return;
    setLeaving(true);
    setSea(false);
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
        // Esc does not interrupt a connection under way.
        if (!ref.current?.querySelector('[aria-busy="true"]')) close();
      }}
    >
      <SeaBackdrop up={sea} />
      <div className="ob-card" key={step} tabIndex={-1}>
        {step === 'agent' ? (
          <AgentStep next={() => (library ? setStep('library') : close())} titleId="ob-title" />
        ) : (
          <Connect connected={close} title="Bring your library" lead="Paste the git repository with your skills, flows and sessions. An empty one starts a new library." titleId="ob-title">
            <button type="button" className="ob-skip" onClick={close}>
              Not now
            </button>
          </Connect>
        )}
      </div>
      {/* A modal dialog hides the page's notifications beneath it; these show above. */}
      <Toaster />
    </dialog>
  );
}
