import { Sea } from './Sea';

/** Orla's loading: a small piece of its sea, rolling, with what is on its way. */
export function Loader({ label }: { label: string }) {
  return (
    <div className="loader" role="status" aria-live="polite">
      <span className="loader-sea" aria-hidden="true">
        <Sea state="idle" rest={0.42} swell={0.36} wavelength={0.22} />
      </span>
      <span className="loader-label">{label}</span>
    </div>
  );
}
