import '@/styles/new-chat.css';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { api, ApiError } from '@/lib/api';
import { AttachmentTray, useAttachments } from './Attachments';
import { Check, ChevronRight, FileIcon, FolderIcon, Paperclip, Plus, Send, Stop } from './Icons';
import { Tooltip } from './Tooltip';
import { Sea, type SeaState } from './Sea';
import { toast } from './Toast';
import { UsageRing } from './Usage';

// The prompt box, for a new session and for the next prompt of one: the text, attached files,
// and Claude Code's own choices (model, effort, ultracode, permission mode), which persist.

type Model = { id: string; name: string };
// What Claude Code offers first, in its order; the digit picks one while the menu is open.
const MODELS: Model[] = [
  { id: 'opus', name: 'Opus 5.5' },
  { id: 'fable', name: 'Fable 5.1' },
  { id: 'sonnet', name: 'Sonnet 5.5' },
  { id: 'haiku', name: 'Haiku 5.5' },
];
const MORE_MODELS: Model[] = [
  { id: 'claude-haiku-4-5', name: 'Haiku 4.5' },
  { id: 'claude-sonnet-5', name: 'Sonnet 5' },
  { id: 'claude-opus-5', name: 'Opus 5' },
  { id: 'claude-fable-5', name: 'Fable 5' },
  { id: 'claude-opus-4-8', name: 'Opus 4.8' },
  { id: 'claude-opus-4-7', name: 'Opus 4.7' },
  { id: 'claude-opus-4-6', name: 'Opus 4.6' },
  { id: 'claude-sonnet-4-6', name: 'Sonnet 4.6' },
];
const EFFORTS: { id: string; name: string; note?: ReactNode }[] = [
  { id: 'low', name: 'Low' },
  { id: 'medium', name: 'Medium', note: <span className="nc-badge">Recommended</span> },
  { id: 'high', name: 'High' },
  { id: 'xhigh', name: 'Extra' },
  {
    id: 'max',
    name: 'Max',
    note: (
      <span className="nc-badge" data-tone="wait">
        3.5× or more usage
      </span>
    ),
  },
];
// In Claude Code's Shift+Tab order.
const MODES = [
  { id: 'default', name: 'Ask permissions', hint: 'Every tool waits for your answer, here or on your phone' },
  { id: 'acceptEdits', name: 'Accept edits', hint: 'File edits go through; commands still ask' },
  { id: 'plan', name: 'Plan mode', hint: 'Reads and plans, changes nothing' },
  { id: 'auto', name: 'Auto mode', hint: 'The agent decides what needs asking' },
  { id: 'bypassPermissions', name: 'Bypass permissions', hint: 'Nothing asks. Use it on a machine you can lose' },
];

const modelName = (id: string) => [...MODELS, ...MORE_MODELS].find((m) => m.id === id)?.name ?? id;
const effortName = (id: string) => EFFORTS.find((e) => e.id === id)?.name ?? id;
const modeName = (id: string) => MODES.find((m) => m.id === id)?.name ?? id;

export interface Prefs {
  model: string;
  effort: string;
  mode: string;
  ultracode: boolean;
}
const PREFS = 'orla.new-chat';
const DEFAULTS: Prefs = { model: 'opus', effort: 'medium', mode: 'default', ultracode: false };

function usePrefs(): [Prefs, (p: Partial<Prefs>) => void] {
  const [prefs, setPrefs] = useState<Prefs>(() => {
    try {
      return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(PREFS) ?? '{}') };
    } catch {
      return DEFAULTS;
    }
  });
  const set = useCallback((p: Partial<Prefs>) => {
    setPrefs((old) => {
      const next = { ...old, ...p };
      try {
        localStorage.setItem(PREFS, JSON.stringify(next));
      } catch {}
      return next;
    });
  }, []);
  return [prefs, set];
}

// ---------------------------------------------------------------------------------------------
// The menu: models, then effort and older models as submenus, the way Claude Code lays them out.

type Sub = 'effort' | 'more' | null;

function ModelMenu({ prefs, set, onClose }: { prefs: Prefs; set: (p: Partial<Prefs>) => void; onClose: () => void }) {
  const [sub, setSub] = useState<Sub>(null);
  const root = useRef<HTMLDivElement>(null);
  const subRef = useRef<HTMLDivElement>(null);

  const items = (el: HTMLElement | null) => Array.from(el?.querySelectorAll<HTMLElement>('[role^="menuitem"]') ?? []);
  // Takes a getter: the submenu is not rendered yet when it is asked to open.
  const focusIn = (el: () => HTMLElement | null, i = 0) => setTimeout(() => items(el())[i]?.focus(), 0);

  useEffect(() => {
    const first = items(root.current).findIndex((x) => x.getAttribute('aria-checked') === 'true');
    focusIn(() => root.current, Math.max(0, first));
    const away = (e: PointerEvent) => !(e.target as HTMLElement).closest('.nc-menu, .nc-model') && onClose();
    addEventListener('pointerdown', away);
    return () => removeEventListener('pointerdown', away);
  }, [onClose]);

  const pick = (model: string) => {
    set({ model });
    onClose();
  };

  // Arrows move, Right opens a submenu, Left or Escape goes back, digits pick a model.
  const onKey = (e: KeyboardEvent) => {
    const inSub = Boolean(subRef.current?.contains(document.activeElement));
    const list = items(inSub ? subRef.current : root.current);
    const i = list.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const d = e.key === 'ArrowDown' ? 1 : -1;
      list[(i + d + list.length) % list.length]?.focus();
    } else if (e.key === 'ArrowRight' && !inSub) {
      const s = (document.activeElement as HTMLElement)?.dataset.sub as Sub;
      if (s) {
        e.preventDefault();
        setSub(s);
        focusIn(() => subRef.current);
      }
    } else if ((e.key === 'ArrowLeft' || e.key === 'Escape') && inSub) {
      e.preventDefault();
      e.stopPropagation();
      const s = sub;
      setSub(null);
      setTimeout(() => root.current?.querySelector<HTMLElement>(`[data-sub="${s}"]`)?.focus(), 0);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    } else if (/^[1-4]$/.test(e.key) && !inSub) {
      e.preventDefault();
      pick(MODELS[Number(e.key) - 1]!.id);
    }
  };

  const opener = (s: Exclude<Sub, null>, label: string, value?: string) => (
    <button
      type="button"
      role="menuitem"
      aria-haspopup="menu"
      aria-expanded={sub === s}
      data-sub={s}
      className="nc-item"
      data-open={sub === s || undefined}
      onPointerEnter={(e) => {
        e.currentTarget.focus();
        setSub(s);
      }}
      onClick={() => {
        setSub(s);
        focusIn(() => subRef.current);
      }}
    >
      <span className="nc-item-name">{label}</span>
      {value && <span className="nc-item-value">{value}</span>}
      <ChevronRight />
    </button>
  );

  return (
    <div className="nc-menu-wrap" onKeyDown={onKey}>
      <div ref={root} className="nc-menu" role="menu" aria-label="Model">
        {MODELS.map((m, i) => (
          <button
            key={m.id}
            type="button"
            role="menuitemradio"
            aria-checked={prefs.model === m.id}
            className="nc-item"
            style={{ animationDelay: `${i * 18}ms` }}
            onPointerEnter={(e) => {
              e.currentTarget.focus();
              setSub(null);
            }}
            onClick={() => pick(m.id)}
          >
            <span className="nc-item-name">{m.name}</span>
            {prefs.model === m.id ? <Check /> : <kbd className="nc-item-key">{i + 1}</kbd>}
          </button>
        ))}
        <div className="nc-sep" role="separator" />
        {opener('effort', 'Effort', effortName(prefs.effort))}
        <div className="nc-sep" role="separator" />
        {opener('more', 'More models', MORE_MODELS.some((m) => m.id === prefs.model) ? modelName(prefs.model) : undefined)}
      </div>

      {sub && (
        <div ref={subRef} className="nc-menu nc-sub" data-sub={sub} role="menu" aria-label={sub === 'effort' ? 'Effort' : 'More models'}>
          <button type="button" role="menuitem" className="nc-item nc-back" onClick={() => setSub(null)}>
            <ChevronRight />
            <span className="nc-item-name">{sub === 'effort' ? 'Effort' : 'More models'}</span>
          </button>
          {sub === 'effort' ? (
            <>
              <p className="nc-sub-note">More effort means fuller answers, but it takes longer and uses your limits faster.</p>
              {EFFORTS.map((x, i) => (
                <button
                  key={x.id}
                  type="button"
                  role="menuitemradio"
                  aria-checked={prefs.effort === x.id}
                  className="nc-item"
                  style={{ animationDelay: `${i * 18}ms` }}
                  onPointerEnter={(e) => e.currentTarget.focus()}
                  onClick={() => set({ effort: x.id })}
                >
                  <span className="nc-item-name">{x.name}</span>
                  {x.note}
                  <span className="nc-grow" />
                  {prefs.effort === x.id && <Check />}
                </button>
              ))}
              <div className="nc-sep" role="separator" />
              <button
                type="button"
                role="menuitemcheckbox"
                aria-checked={prefs.ultracode}
                className="nc-item nc-ultra"
                onPointerEnter={(e) => e.currentTarget.focus()}
                onClick={() => set({ ultracode: !prefs.ultracode })}
              >
                <span className="nc-item-name">Ultracode</span>
                <span className="nc-grow" />
                <span className="nc-switch" aria-hidden="true" />
              </button>
            </>
          ) : (
            MORE_MODELS.map((m, i) => (
              <button
                key={m.id}
                type="button"
                role="menuitemradio"
                aria-checked={prefs.model === m.id}
                className="nc-item"
                style={{ animationDelay: `${i * 14}ms` }}
                onPointerEnter={(e) => e.currentTarget.focus()}
                onClick={() => pick(m.id)}
              >
                <span className="nc-item-name">{m.name}</span>
                {prefs.model === m.id && <Check />}
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ModeMenu({ mode, set, onClose }: { mode: string; set: (mode: string) => void; onClose: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const list = Array.from(root.current?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') ?? []);
    setTimeout(() => (list.find((x) => x.getAttribute('aria-checked') === 'true') ?? list[0])?.focus(), 0);
    const away = (e: PointerEvent) => !(e.target as HTMLElement).closest('.nc-menu, .nc-mode') && onClose();
    addEventListener('pointerdown', away);
    return () => removeEventListener('pointerdown', away);
  }, [onClose]);
  const onKey = (e: KeyboardEvent) => {
    const list = Array.from(root.current?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') ?? []);
    const i = list.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      list[(i + (e.key === 'ArrowDown' ? 1 : -1) + list.length) % list.length]?.focus();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    }
  };
  return (
    <div className="nc-menu-wrap nc-menu-wrap-end" onKeyDown={onKey}>
      <div ref={root} className="nc-menu nc-modes" role="menu" aria-label="Permission mode">
        {MODES.map((m, i) => (
          <button
            key={m.id}
            type="button"
            role="menuitemradio"
            aria-checked={mode === m.id}
            className="nc-item nc-item-tall"
            data-mode={m.id}
            style={{ animationDelay: `${i * 18}ms` }}
            onPointerEnter={(e) => e.currentTarget.focus()}
            onClick={() => {
              set(m.id);
              onClose();
            }}
          >
            <span className="nc-item-text">
              <span className="nc-item-name">{m.name}</span>
              <span className="nc-item-hint">{m.hint}</span>
            </span>
            {mode === m.id && <Check />}
          </button>
        ))}
        <p className="nc-sub-note nc-menu-foot">
          <kbd>Shift</kbd> <kbd>Tab</kbd> cycles them from the prompt
        </p>
      </div>
    </div>
  );
}

function AddMenu({ pick, onClose }: { pick: () => void; onClose: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setTimeout(() => root.current?.querySelector<HTMLElement>('[role="menuitem"]')?.focus(), 0);
    const away = (e: PointerEvent) => !(e.target as HTMLElement).closest('.nc-menu, .nc-add') && onClose();
    addEventListener('pointerdown', away);
    return () => removeEventListener('pointerdown', away);
  }, [onClose]);
  return (
    <div
      className="nc-menu-wrap"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <div ref={root} className="nc-menu" role="menu" aria-label="Add">
        <button type="button" role="menuitem" className="nc-item" onClick={pick}>
          <Paperclip />
          <span className="nc-item-name">Add files or photos</span>
          <kbd className="nc-item-key nc-item-chord">Ctrl+U</kbd>
        </button>
      </div>
    </div>
  );
}


/** Puts text into the prompt box on screen, at its caret: a reference to a line, say. */
export function insertIntoPrompt(text: string, replace?: string) {
  dispatchEvent(new CustomEvent<{ text: string; replace?: string }>('orla-insert', { detail: { text, replace } }));
}

/** Starts the prompt box with a command such as /newflow, for the owner to complete and send. */
export function commandInPrompt(text: string) {
  dispatchEvent(new CustomEvent<{ text: string }>('orla-command', { detail: { text } }));
}

type Hit = { path: string; dir?: boolean };

/** What an @ is pointing at as it is typed: where it starts and what follows it. */
function mentionAt(value: string, caret: number): { start: number; query: string } | null {
  const m = /(^|\s)@([^\s@]*)$/.exec(value.slice(0, caret));
  return m ? { start: caret - m[2]!.length - 1, query: m[2]! } : null;
}

/** What a send carries. */
export interface Outgoing {
  prompt: string;
  attachments?: string[];
  model: string;
  effort: string;
  mode: string;
  ultracode?: boolean;
}

export function PromptBox({
  draftKey,
  placeholder,
  onSend,
  flood = false,
  closed,
  busy = false,
  autoFocus = true,
  onSending,
  onQueue,
  onStop,
  queued = 0,
  cwd = '',
}: {
  /** Where the draft and its files are kept, so a reload loses nothing. */
  draftKey: string;
  placeholder: string;
  /** Sends; a throw shows its message and keeps the draft. */
  onSend: (o: Outgoing) => Promise<void>;
  /** After a send the sea covers the box and stays (the page is about to change). */
  flood?: boolean;
  /** Why no more prompts are taken, if they are not. */
  closed?: string;
  /** A run is in progress: the next prompt waits for it. */
  busy?: boolean;
  autoFocus?: boolean;
  onSending?: (sending: boolean) => void;
  /** While a run is in progress, a send goes here instead: the message waits in line. */
  onQueue?: (o: Outgoing) => void;
  /** Stops the run in progress (the stop button, Esc). */
  onStop?: () => void;
  /** Messages waiting in line: Enter on an empty box stops the run so they go now. */
  queued?: number;
  /** The folder an @ searches: the session's. Empty is the home directory. */
  cwd?: string;
}) {
  const [prefs, set] = usePrefs();
  const [prompt, setPrompt] = useState(() => {
    try {
      return localStorage.getItem(draftKey) ?? '';
    } catch {
      return '';
    }
  });
  useEffect(() => {
    try {
      if (prompt) localStorage.setItem(draftKey, prompt);
      else localStorage.removeItem(draftKey);
    } catch {}
  }, [prompt, draftKey]);
  const [menu, setMenu] = useState<'add' | 'model' | 'mode' | null>(null);
  const files = useAttachments(`${draftKey}.files`);
  const picker = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [sending, setSendingState] = useState(false);
  const setSending = (v: boolean) => {
    setSendingState(v);
    onSending?.(v);
  };
  const [sea, setSea] = useState<SeaState>('recede');
  const [sentCount, setSentCount] = useState(0);
  const box = useRef<HTMLTextAreaElement>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const pickFiles = useCallback(() => {
    setMenu(null);
    picker.current?.click();
  }, []);
  // Ctrl+U attaches from anywhere on the page, as in Claude.
  useEffect(() => {
    if (closed) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'u') {
        e.preventDefault();
        pickFiles();
      }
    };
    addEventListener('keydown', onKey);
    return () => removeEventListener('keydown', onKey);
  }, [pickFiles, closed]);

  // The most Claude Code can be asked for: the sea comes up in the box.
  const power = prefs.ultracode || prefs.model === 'fable' || prefs.effort === 'max';
  useEffect(() => {
    if (!sending) setSea(power ? 'idle' : 'recede');
  }, [power, sending]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 320)}px`;
  }, [prompt]);

  // Arriving, the keyboard is already in the field, at the end of whatever draft is there.
  useEffect(() => {
    const el = box.current;
    if (!el || !autoFocus || closed) return;
    el.focus();
    const end = el.value.length;
    el.setSelectionRange(end, end);
    el.scrollTop = el.scrollHeight;
  }, [autoFocus, closed]);

  // Text put in from elsewhere (a line picked in Changes) goes in at the caret, spaced.
  useEffect(() => {
    const on = (e: Event) => {
      const el = box.current;
      if (!el || closed) return;
      const { text, replace } = (e as CustomEvent<{ text: string; replace?: string }>).detail;
      // A range picked after a line replaces that line's reference, when it is still there.
      const i = replace ? el.value.lastIndexOf(replace) : -1;
      if (replace && i >= 0) {
        const next = el.value.slice(0, i) + text + el.value.slice(i + replace.length);
        setPrompt(next);
        setTimeout(() => {
          el.focus();
          el.setSelectionRange(i + text.length + 1, i + text.length + 1);
        }, 0);
        return;
      }
      const at = el === document.activeElement ? el.selectionStart : el.value.length;
      const before = el.value.slice(0, at);
      const after = el.value.slice(at);
      const piece = `${before && !/\s$/.test(before) ? ' ' : ''}${text} `;
      const next = before + piece + after;
      setPrompt(next);
      setTimeout(() => {
        el.focus();
        const caret = before.length + piece.length;
        el.setSelectionRange(caret, caret);
      }, 0);
    };
    addEventListener('orla-insert', on);
    return () => removeEventListener('orla-insert', on);
  }, [closed]);

  // @ points at a file or folder: a list of matches opens above the box as it is typed.
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [hits, setHits] = useState<Hit[]>([]);
  const [hitIndex, setHitIndex] = useState(0);
  const readMention = () => {
    const el = box.current;
    if (!el) return;
    setMention(mentionAt(el.value, el.selectionStart));
  };
  useEffect(() => {
    // A new query starts from an empty list, so Enter never picks what the last one found.
    setHits([]);
    if (!mention) return;
    let stale = false;
    const t = setTimeout(() => {
      api.files(cwd, mention.query).then(
        (r) => {
          if (stale) return;
          setHits(r);
          setHitIndex(0);
        },
        () => {},
      );
    }, 70);
    return () => {
      stale = true;
      clearTimeout(t);
    };
  }, [mention?.query, mention !== null, cwd]);
  // The arrows move through the list; it scrolls to keep the one picked in view.
  // / at the start opens the commands: every skill, Orla's own (/newflow…) and the owner's.
  const [slash, setSlash] = useState<string | null>(null);
  const [commands, setCommands] = useState<{ name: string; description?: string; orla?: boolean }[] | null>(null);
  const [slashIndex, setSlashIndex] = useState(0);
  const readSlash = (value: string, caret: number) => {
    const m = /^\/([\w:.-]*)$/.exec(value.slice(0, caret));
    setSlash(m ? m[1]! : null);
    setSlashIndex(0);
  };
  useEffect(() => {
    if (slash === null || commands) return;
    api.skills(cwd).then(
      (r) => setCommands(r.skills.map((s) => ({ name: s.name, description: s.description, orla: s.orla }))),
      () => setCommands([]),
    );
  }, [slash, commands, cwd]);
  const slashHits = useMemo(() => {
    if (slash === null || !commands) return [];
    const q = slash.toLowerCase();
    const starts = commands.filter((c) => c.name.toLowerCase().startsWith(q));
    const inside = commands.filter((c) => !c.name.toLowerCase().startsWith(q) && c.name.toLowerCase().includes(q));
    return [...starts.sort((a, b) => Number(!!b.orla) - Number(!!a.orla) || a.name.localeCompare(b.name)), ...inside].slice(0, 8);
  }, [slash, commands]);
  const pickCommand = (name: string) => {
    const el = box.current;
    if (!el) return;
    const rest = el.value.slice(el.selectionStart).replace(/^\S*/, '');
    const head = `/${name} `;
    setPrompt(head + rest.trimStart());
    setSlash(null);
    setTimeout(() => {
      el.focus();
      el.setSelectionRange(head.length, head.length);
    }, 0);
  };

  const listRef = useRef<HTMLUListElement>(null);
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [hitIndex, hits]);
  const pickHit = (h: Hit) => {
    const el = box.current;
    if (!el || !mention) return;
    const caret = el.selectionStart;
    const piece = `@${h.path}${h.dir ? '' : ' '}`;
    const next = el.value.slice(0, mention.start) + piece + el.value.slice(caret);
    setPrompt(next);
    const at = mention.start + piece.length;
    setTimeout(() => {
      el.focus();
      el.setSelectionRange(at, at);
      // A folder keeps the list open, now inside it.
      setMention(h.dir ? mentionAt(next, at) : null);
    }, 0);
  };

  const closeMenu = useCallback(() => {
    setMenu(null);
    box.current?.focus();
  }, []);

  // A command put in from elsewhere on the page, such as /newflow: it goes at the start, ready to
  // be completed and sent by the owner.
  useEffect(() => {
    const on = (e: Event) => {
      const { text } = (e as CustomEvent<{ text: string }>).detail;
      const el = box.current;
      const cur = el?.value ?? '';
      // Another command already there is replaced; text being written stays after it.
      const rest = cur.trimStart().startsWith('/') ? '' : cur;
      const next = `${text} ${rest}`;
      setPrompt(next);
      setTimeout(() => {
        el?.focus();
        el?.setSelectionRange(text.length + 1, text.length + 1);
      }, 0);
    };
    addEventListener('orla-command', on);
    return () => removeEventListener('orla-command', on);
  }, []);

  // While a run is in progress, Enter puts the message in line; Enter again, on the empty box,
  // stops the run so the line goes now.
  const send = async () => {
    const text = prompt.trim();
    if (busy && !text && queued > 0 && onStop) return onStop();
    if (!text || sending || closed) return;
    if (files.uploading) return toast('Wait for the files to finish uploading.');
    if (files.failed) return toast('Remove the files that failed to upload, or attach them again.');
    if (busy) {
      if (!onQueue) return toast('A run is in progress. Send this when it ends, or stop it.');
      onQueue({
        prompt: text,
        attachments: files.ids.length ? files.ids : undefined,
        model: prefs.model,
        effort: prefs.effort,
        mode: prefs.mode,
        ultracode: prefs.ultracode || undefined,
      });
      setPrompt('');
      files.clear();
      try {
        localStorage.removeItem(draftKey);
        localStorage.removeItem(`${draftKey}.files`);
      } catch {}
      box.current?.focus();
      return;
    }
    setSending(true);
    setMenu(null);
    setSea('busy');
    const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const tide = new Promise((r) => setTimeout(r, reduce ? 0 : 650));
    try {
      await onSend({
        prompt: text,
        attachments: files.ids.length ? files.ids : undefined,
        model: prefs.model,
        effort: prefs.effort,
        mode: prefs.mode,
        ultracode: prefs.ultracode || undefined,
      });
      // Sent: the draft is done with.
      try {
        localStorage.removeItem(draftKey);
        localStorage.removeItem(`${draftKey}.files`);
      } catch {}
      await tide;
      if (!alive.current) return;
      if (flood) {
        setSea('ok');
        return;
      }
      setPrompt('');
      files.clear();
      setSending(false);
      setSentCount((n) => n + 1);
      box.current?.focus();
    } catch (err) {
      if (!alive.current) return;
      setSea(power ? 'idle' : 'recede');
      toast(err instanceof ApiError || err instanceof Error ? err.message : 'Orla did not answer. Try again.');
      setSending(false);
    }
  };

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (slash !== null && slashHits.length > 0) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        setSlashIndex((i) => (i + (e.key === 'ArrowDown' ? 1 : -1) + slashHits.length) % slashHits.length);
        return;
      }
      if ((e.key === 'Enter' && !e.shiftKey) || e.key === 'Tab') {
        e.preventDefault();
        pickCommand(slashHits[slashIndex]!.name);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setSlash(null);
        return;
      }
    }
    if (mention && hits.length > 0) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        setHitIndex((i) => (i + (e.key === 'ArrowDown' ? 1 : -1) + hits.length) % hits.length);
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        pickHit(hits[hitIndex]!);
        return;
      }
    }
    // While an @ is open, Enter and Escape belong to it: they never send. With no list yet,
    // they only close it.
    if (mention && (e.key === 'Escape' || (e.key === 'Enter' && !e.shiftKey))) {
      e.preventDefault();
      setMention(null);
      return;
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send();
    } else if (e.key === 'Escape' && busy && onStop && !menu) {
      e.preventDefault();
      onStop();
    } else if (e.key === 'Tab' && e.shiftKey && !sending) {
      e.preventDefault();
      const i = MODES.findIndex((m) => m.id === prefs.mode);
      set({ mode: MODES[(i + 1) % MODES.length]!.id });
    }
  };

  return (
    <>
      <form
        className="nc-box"
        data-power={power || undefined}
        data-sending={sending || undefined}
        data-dragging={dragging || undefined}
        data-closed={closed ? '' : undefined}
        onDragOver={(e) => {
          if (!e.dataTransfer.types.includes('Files') || sending || closed) return;
          e.preventDefault();
          setDragging(true);
        }}
        onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget as Node) && setDragging(false)}
        onDrop={(e) => {
          setDragging(false);
          if (!e.dataTransfer.files.length || sending || closed) return;
          e.preventDefault();
          files.add(e.dataTransfer.files);
        }}
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <div className="nc-water" aria-hidden="true">
          <Sea state={sea} rest={0.3} />
        </div>
        <AttachmentTray items={files.items} remove={files.remove} />
        {slash !== null && slashHits.length > 0 && (
          <ul className="mention slash" role="listbox" aria-label="Commands">
            {slashHits.map((c, i) => (
              <li
                key={c.name}
                role="option"
                aria-selected={i === slashIndex}
                className="mention-item"
                ref={i === slashIndex ? (el) => el?.scrollIntoView({ block: 'nearest' }) : undefined}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pickCommand(c.name);
                }}
                onMouseEnter={() => setSlashIndex(i)}
              >
                <span className="slash-name">/{c.name}</span>
                {c.description && <span className="slash-desc">{c.description}</span>}
              </li>
            ))}
          </ul>
        )}
        {mention && hits.length > 0 && (
          <ul className="mention" role="listbox" aria-label="Files" ref={listRef}>
            {hits.map((h, i) => (
              <li
                key={h.path}
                role="option"
                aria-selected={i === hitIndex}
                className="mention-item"
                onMouseDown={(e) => {
                  e.preventDefault();
                  pickHit(h);
                }}
                onMouseEnter={() => setHitIndex(i)}
              >
                {h.dir ? <FolderIcon /> : <FileIcon />}
                <span className="mention-dir">{h.path.replace(/[^/]+\/?$/, '')}</span>
                <span className="mention-name">{h.path.match(/[^/]+\/?$/)?.[0] ?? h.path}</span>
              </li>
            ))}
          </ul>
        )}
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files) files.add(e.target.files);
            e.target.value = '';
            box.current?.focus();
          }}
        />
        <textarea
          ref={box}
          autoFocus={autoFocus}
          rows={autoFocus ? 2 : 1}
          value={prompt}
          onChange={(e) => {
            setPrompt(e.target.value);
            setMention(mentionAt(e.target.value, e.target.selectionStart));
            readSlash(e.target.value, e.target.selectionStart);
          }}
          onClick={readMention}
          onBlur={() =>
            setTimeout(() => {
              setMention(null);
              setSlash(null);
            }, 150)
          }
          onKeyDown={onKey}
          onPaste={(e) => {
            // A screenshot or a copied file is attached; text pastes as text.
            if (e.clipboardData.files.length === 0 || sending || closed) return;
            e.preventDefault();
            files.add(e.clipboardData.files);
          }}
          placeholder={closed || placeholder}
          aria-label="Prompt"
          readOnly={sending}
          disabled={Boolean(closed)}
        />
        {!closed && (
          <div className="nc-bar">
            <div className="nc-anchor">
              <button
                type="button"
                className="nc-pill nc-add"
                aria-label="Add files or photos"
                aria-haspopup="menu"
                aria-expanded={menu === 'add'}
                onClick={() => setMenu(menu === 'add' ? null : 'add')}
                disabled={sending}
              >
                <Plus />
              </button>
              {menu === 'add' && <AddMenu pick={pickFiles} onClose={closeMenu} />}
            </div>
            <div className="nc-anchor">
              <button
                type="button"
                className="nc-pill nc-model"
                aria-haspopup="menu"
                aria-expanded={menu === 'model'}
                onClick={() => setMenu(menu === 'model' ? null : 'model')}
                disabled={sending}
              >
                <span className="nc-pill-label">{modelName(prefs.model)}</span>
                <span className="nc-pill-dim">{effortName(prefs.effort)}</span>
                {prefs.ultracode && <span className="nc-pill-ultra">Ultracode</span>}
                <span className="nc-pill-chev" aria-hidden="true">
                  <ChevronRight />
                </span>
              </button>
              {menu === 'model' && <ModelMenu prefs={prefs} set={set} onClose={closeMenu} />}
            </div>
            <div className="nc-anchor">
              <button
                type="button"
                className="nc-pill nc-mode"
                data-mode={prefs.mode}
                aria-haspopup="menu"
                aria-expanded={menu === 'mode'}
                onClick={() => setMenu(menu === 'mode' ? null : 'mode')}
                disabled={sending}
              >
                <span className="nc-pill-label">{modeName(prefs.mode)}</span>
                <span className="nc-pill-chev" aria-hidden="true">
                  <ChevronRight />
                </span>
              </button>
              {menu === 'mode' && <ModeMenu mode={prefs.mode} set={(mode) => set({ mode })} onClose={closeMenu} />}
            </div>
            <span className="nc-grow" />
            <UsageRing refreshKey={sentCount} />
            {busy && onStop && !prompt.trim() ? (
              <Tooltip label="Stop · Esc" side="top">
                <button type="button" className="nc-send nc-stop" onClick={onStop} aria-label="Stop">
                  <Stop />
                </button>
              </Tooltip>
            ) : (
              <button className="nc-send" disabled={sending || (busy && !onQueue) || !prompt.trim() || files.uploading} aria-label={busy ? 'Queue' : 'Send'}>
                {busy ? <EnterKey /> : <Send />}
              </button>
            )}
          </div>
        )}
      </form>
    </>
  );
}

const EnterKey = () => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M13 3.5v4.5a2 2 0 0 1-2 2H3.5" />
    <path d="M6 7.5 3.5 10 6 12.5" />
  </svg>
);
