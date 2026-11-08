import { useEffect, useState } from 'react';
import { Check, Cross } from './Icons';

type Tone = 'error' | 'info' | 'ok';
interface Note {
  id: number;
  text: string;
  tone: Tone;
}

/** Shows a short note at the corner of the screen, from anywhere. Errors stay a little longer. */
export function toast(text: string, tone: Tone = 'error') {
  dispatchEvent(new CustomEvent<Omit<Note, 'id'>>('orla-toast', { detail: { text, tone } }));
}

/** Where the notes appear; mounted once by the panel. */
export function Toaster() {
  const [notes, setNotes] = useState<Note[]>([]);
  useEffect(() => {
    let n = 0;
    const on = (e: Event) => {
      const d = (e as CustomEvent<Omit<Note, 'id'>>).detail;
      const id = ++n;
      setNotes((list) => [...list.filter((x) => x.text !== d.text), { id, ...d }].slice(-4));
      setTimeout(() => setNotes((list) => list.filter((x) => x.id !== id)), d.tone === 'error' ? 6000 : 3000);
    };
    addEventListener('orla-toast', on);
    return () => removeEventListener('orla-toast', on);
  }, []);
  return (
    <div className="toaster" role="region" aria-label="Notifications">
      {notes.map((t) => (
        <div key={t.id} className="toast" data-tone={t.tone} role={t.tone === 'error' ? 'alert' : 'status'}>
          {t.tone === 'error' ? <Cross /> : <Check />}
          <span>{t.text}</span>
          <button type="button" className="toast-x" onClick={() => setNotes((l) => l.filter((x) => x.id !== t.id))} aria-label="Dismiss">
            <Cross />
          </button>
        </div>
      ))}
    </div>
  );
}
