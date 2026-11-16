import '@/styles/sync.css';
import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import type { EnvLink, EnvList, Environment } from '@/lib/types';
import { ask } from './ConfirmDialog';
import { EmptyState } from './EmptyState';
import { Pencil, Plus, Trash } from './Icons';
import { SeaDialog } from './SeaDialog';
import { toast } from './Toast';

const fail = (err: unknown, fallback: string) => {
  const m = err instanceof Error ? err.message : '';
  toast(m ? m.charAt(0).toUpperCase() + m.slice(1) + '.' : fallback);
};

/**
 * Environments: named sets of variables, kept in the library with their values sealed, and
 * written into the checkouts of the repositories each one is linked to.
 */
export function EnvView() {
  const [list, setList] = useState<EnvList | null>(null);
  const [editing, setEditing] = useState<string | null>(null); // a name, or '' for a new one
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.envs().then(setList, (err) => {
      fail(err, 'Orla could not read the environments.');
      setList({ environments: [], repos: [], locked: false });
    });
  }, []);

  const save = async (name: string, values: Record<string, string>, links: EnvLink[]) => {
    setBusy(true);
    try {
      setList(await api.saveEnv(name, values, links));
      setEditing(null);
    } catch (err) {
      fail(err, 'Orla could not save the environment.');
    } finally {
      setBusy(false);
    }
  };
  const remove = async (e: Environment) => {
    const ok = await ask({ title: `Delete ${e.name}?`, body: 'Its variables leave the library on every machine. Files already written stay until their next run.', confirm: 'Delete', danger: true });
    if (!ok) return;
    try {
      setList(await api.removeEnv(e.name));
    } catch (err) {
      fail(err, 'Orla could not delete it.');
    }
  };

  if (!list) return <div className="au-wrap" role="status" aria-label="Reading the environments" />;

  const current = list.environments.find((e) => e.name === editing);
  const dialog = (
    <SeaDialog open={editing !== null} onClose={() => setEditing(null)} busy={busy} labelledBy="ev-title" className="mc-dialog ev-dialog">
      {editing !== null && <EnvForm key={editing} env={current} repos={list.repos} busy={busy} onCancel={() => setEditing(null)} onSave={save} />}
    </SeaDialog>
  );

  if (list.environments.length === 0)
    return (
      <>
        {dialog}
        <EmptyState
          title="No environments yet"
          art={
            <span className="ev-art" aria-hidden="true">
              {[38, 52, 30].map((w, i) => (
                <span key={i} className="ev-art-line">
                  <b style={{ width: w }} />
                  <i />
                </span>
              ))}
            </span>
          }
          action={
            <button type="button" className="btn btn-primary" onClick={() => setEditing('')}>
              <Plus />
              New environment
            </button>
          }
        >
          Variables your apps read, like a .env. Orla writes them into each repository you link, and they travel encrypted.
        </EmptyState>
      </>
    );

  return (
    <div className="au-wrap">
      {dialog}
      <div className="au-col sy-col">
        <section className="au-section">
          <div className="sy-head">
            <h3>Environments</h3>
            <button type="button" className="fl-act" onClick={() => setEditing('')}>
              <Plus />
              New
            </button>
          </div>
          {list.locked && <p className="au-note mc-note">This machine cannot read the values yet. They open once another machine that can syncs.</p>}
          <ul className="au-list">
            {list.environments.map((e) => (
              <li key={e.name} className="au-row sy-row">
                <div className="au-main">
                  <span className="au-name au-name-static">
                    <span className="au-text">
                      <span className="au-title">{e.name}</span>
                      <span className="au-desc mc-where">{e.links.length ? e.links.map((l) => `${l.repo.split('/').slice(1).join('/')} → ${l.path}`).join(' · ') : 'Not linked to a repository'}</span>
                    </span>
                  </span>
                  <span className="sy-side">
                    <span className="sy-meta">
                      <span className="mc-state">{e.keys.length === 1 ? '1 variable' : `${e.keys.length} variables`}</span>
                    </span>
                    <span className="sy-acts">
                      <button type="button" className="fl-act" disabled={list.locked} onClick={() => setEditing(e.name)} aria-label={`Edit ${e.name}`}>
                        <Pencil />
                      </button>
                      <button type="button" className="fl-act" onClick={() => void remove(e)} aria-label={`Delete ${e.name}`}>
                        <Trash />
                      </button>
                    </span>
                  </span>
                </div>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  );
}

interface Row {
  key: string;
  value: string;
}

/** Reads a pasted .env: KEY=value lines, with export, quotes and comments as dotenv allows. */
function parseEnv(text: string): Row[] {
  const out: Row[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim().replace(/^export\s+/, '');
    const m = /^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line);
    if (!m) continue;
    let v = m[2] ?? '';
    if (/^".*"$/.test(v)) v = v.slice(1, -1).replace(/\\n/g, '\n').replace(/\\"/g, '"').replace(/\\\\/g, '\\');
    else if (/^'.*'$/.test(v)) v = v.slice(1, -1);
    else v = v.replace(/\s+#.*$/, '');
    out.push({ key: m[1] ?? '', value: v });
  }
  return out;
}

interface Var {
  key: string;
  value: string;
  /** Saved before: its value is never shown, and left empty it keeps what was saved. */
  saved: boolean;
}

const blank = (): Var => ({ key: '', value: '', saved: false });
// The list always ends in an empty row, so a new variable is typed without a button.
const withBlank = (vars: Var[]) => (vars.length && !vars[vars.length - 1]!.key && !vars[vars.length - 1]!.value ? vars : [...vars, blank()]);

/**
 * One environment: its variables, one row each, filled by typing or by pasting a .env, and the
 * repositories it goes to. The body scrolls inside the dialog, so any number of variables fits.
 */
function EnvForm({ env, repos, busy, onSave, onCancel }: { env?: Environment; repos: string[]; busy: boolean; onSave: (name: string, values: Record<string, string>, links: EnvLink[]) => void; onCancel: () => void }) {
  const [name, setName] = useState(env?.name ?? '');
  const [vars, setVars] = useState<Var[]>(() => withBlank((env?.keys ?? []).map((key) => ({ key, value: '', saved: true }))));
  const [paste, setPaste] = useState('');
  const [links, setLinks] = useState<EnvLink[]>(env?.links.length ? env.links : [{ repo: repos[0] ?? '', path: '.env' }]);
  const setLink = (i: number, l: Partial<EnvLink>) => setLinks(links.map((x, j) => (j === i ? { ...x, ...l } : x)));
  const setVar = (i: number, v: Partial<Var>) => setVars(withBlank(vars.map((x, j) => (j === i ? { ...x, ...v } : x))));
  const dropVar = (i: number) => setVars(withBlank(vars.filter((_, j) => j !== i)));

  // A pasted .env becomes rows: a line for a variable already listed replaces its value.
  const take = (text: string) => {
    const got = parseEnv(text);
    if (!got.length) {
      toast('No KEY=value lines in that text.');
      return false;
    }
    const next = vars.filter((v) => v.key || v.value).map((v) => {
      const g = got.find((x) => x.key === v.key);
      return g ? { key: v.key, value: g.value, saved: false } : v;
    });
    for (const g of got) if (!next.some((v) => v.key === g.key)) next.push({ key: g.key, value: g.value, saved: false });
    setVars(withBlank(next));
    setPaste('');
    return true;
  };

  const filled = vars.filter((v) => v.key.trim());
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    const bad = filled.find((v) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(v.key.trim()));
    if (bad) {
      toast(`${bad.key} is not a variable name: letters, digits and _, not starting with a digit.`);
      return;
    }
    onSave(
      name.trim(),
      Object.fromEntries(filled.map((v) => [v.key.trim(), v.value])),
      links.filter((l) => l.repo.trim()).map((l) => ({ repo: l.repo.trim(), path: l.path.trim() || '.env' })),
    );
  };

  return (
    <form className="mc-form ev-form" onSubmit={submit} aria-busy={busy}>
      <h2 id="ev-title">{env ? `Edit ${env.name}` : 'New environment'}</h2>
      {/* The fieldset sits inside the scroller: a fieldset does not scroll itself as a flex child. */}
      <div className="ev-body">
        <fieldset className="sy-fieldset" disabled={busy}>
          {!env && (
            <>
              <label className="sy-label" htmlFor="ev-name">
                Name
              </label>
              <input id="ev-name" className="sy-input mc-mono" value={name} onChange={(e) => setName(e.target.value)} placeholder="dev" spellCheck={false} autoComplete="off" autoFocus />
            </>
          )}

          <label className="sy-label" htmlFor="ev-paste">
            Paste a .env
          </label>
          <div className="ev-paste">
            <textarea
              id="ev-paste"
              className="sy-input mc-mono"
              rows={2}
              value={paste}
              onChange={(e) => setPaste(e.target.value)}
              onPaste={(e) => {
                const t = e.clipboardData.getData('text');
                if (parseEnv(t).length) {
                  e.preventDefault();
                  take(t);
                }
              }}
              placeholder={'API_URL=https://…\nDATABASE_URL=postgres://…'}
              spellCheck={false}
              autoComplete="off"
            />
            {paste.trim() && (
              <button type="button" className="btn" onClick={() => take(paste)}>
                Add to variables
              </button>
            )}
          </div>

          <div className="ev-head">
            <span className="sy-label">Variables</span>
            <span className="ev-count">{filled.length === 1 ? '1 variable' : `${filled.length} variables`}</span>
          </div>
          <div className="ev-vars" role="group" aria-label="Variables">
            {vars.map((v, i) => {
              const last = i === vars.length - 1;
              return (
                <div key={i} className="ev-row">
                  <input className="sy-input mc-mono" value={v.key} onChange={(e) => setVar(i, { key: e.target.value })} placeholder={last ? 'NEW_KEY' : 'KEY'} aria-label="Variable name" spellCheck={false} autoComplete="off" />
                  <input
                    className="sy-input mc-mono"
                    value={v.value}
                    onChange={(e) => setVar(i, { value: e.target.value, saved: v.saved && e.target.value === '' })}
                    placeholder={v.saved ? 'Saved · empty keeps it' : 'value'}
                    aria-label={`Value of ${v.key || 'the new variable'}`}
                    spellCheck={false}
                    autoComplete="off"
                  />
                  {last ? (
                    <span aria-hidden="true" />
                  ) : (
                    <button type="button" className="fl-act" onClick={() => dropVar(i)} aria-label={`Remove ${v.key || 'this variable'}`}>
                      <Trash />
                    </button>
                  )}
                </div>
              );
            })}
          </div>

          <span className="sy-label ev-label">Repositories</span>
          <datalist id="ev-repos">
            {repos.map((r) => (
              <option key={r} value={r} />
            ))}
          </datalist>
          <div className="ev-rows">
            {links.map((l, i) => (
              <div key={i} className="ev-row">
                <input className="sy-input mc-mono" value={l.repo} onChange={(e) => setLink(i, { repo: e.target.value })} list="ev-repos" placeholder="github.com/you/app" aria-label="Repository" spellCheck={false} autoComplete="off" />
                <input className="sy-input mc-mono" value={l.path} onChange={(e) => setLink(i, { path: e.target.value })} placeholder=".env" aria-label="File in the repository" spellCheck={false} autoComplete="off" />
                <button type="button" className="fl-act" onClick={() => setLinks(links.filter((_, j) => j !== i))} aria-label={`Unlink ${l.repo || 'this repository'}`}>
                  <Trash />
                </button>
              </div>
            ))}
          </div>
          <button type="button" className="fl-act ev-add" onClick={() => setLinks([...links, { repo: '', path: '.env' }])}>
            <Plus />
            Link a repository
          </button>
        </fieldset>
      </div>
      <div className="mc-acts ev-foot">
        <button type="button" className="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy || !name.trim()}>
          {busy ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
  );
}
