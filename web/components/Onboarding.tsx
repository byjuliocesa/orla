import '@/styles/sync.css';
import { Connect } from './SyncView';

/**
 * The first visit: bring the library from its repository, or start without one. Shown until
 * the owner connects or skips; Library › Sync connects later.
 */
export function Onboarding({ done }: { done: () => void }) {
  return (
    <div className="au-wrap ob-wrap">
      <div className="ob-col">
        <Connect connected={done} title="Bring your library" lead="Paste the git repository with your skills, flows and sessions. An empty one starts a new library.">
          <button type="button" className="ob-skip" onClick={done}>
            Not now
          </button>
        </Connect>
      </div>
    </div>
  );
}
