import '@/styles/sync.css';
import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import type { McpInput, McpList, McpServer, McpView as Server } from '@/lib/types';
import { ask } from './ConfirmDialog';
import { EmptyState } from './EmptyState';
import { Pencil, Plus, Trash } from './Icons';
import { toast } from './Toast';

const STATE: Record<Server['state'], string> = {
  ready: 'Ready',
  locked: 'Waiting for its key',
  taken: 'Name in use',
  error: 'Failed',
};

const fail = (err: unknown, fallback: string) => {
  const m = err instanceof Error ? err.message : '';
  toast(m ? m.charAt(0).toUpperCase() + m.slice(1) + '.' : fallback);
};

/**
 * MCP: the servers every session's agent can reach, kept in the library so they travel. Their
 * keys travel encrypted to this machine's SSH key; Orla sets each one up in Claude.
 */
export function McpView() {
  const [list, setList] = useState<McpList | null>(null);
  const [editing, setEditing] = useState<string | null>(null); // a name, or '' for a new one
  const [busy, setBusy] = useState<string | null>(null);

  const load = () =>
    api.mcp().then(setList, (err) => {
      fail(err, 'Orla could not read the MCP servers.');
      setList({ servers: [], outside: [], locked: false });
    });
  useEffect(() => {
    void load();
  }, []);

  const act = async (key: string, f: () => Promise<McpList>) => {
    setBusy(key);
    try {
      setList(await f());
      return true;
    } catch (err) {
      fail(err, 'Orla could not change the server.');
      return false;
    } finally {
      setBusy(null);
    }
  };

  const remove = async (s: Server) => {
    const ok = await ask({ title: `Remove ${s.name}?`, body: 'It leaves the library and Claude, on every machine.', confirm: 'Remove', danger: true });
    if (ok) await act(s.name, () => api.removeMcp(s.name));
  };

  if (!list) return <div className="au-wrap" role="status" aria-label="Reading the MCP servers" />;
  if (list.servers.length === 0 && list.outside.length === 0 && editing === null)
    return (
      <EmptyState
        title="No MCP servers yet"
        action={
          <button type="button" className="btn btn-primary" onClick={() => setEditing('')}>
            <Plus />
            Add server
          </button>
        }
      >
        Tools the agent reaches, like VX Work. Their keys travel encrypted to your other machines.
      </EmptyState>
    );

  return (
    <div className="au-wrap">
      <div className="au-col sy-col">
        <section className="au-section">
          <div className="sy-head">
            <h3>In the library</h3>
            {editing === null && (
              <button type="button" className="fl-act" onClick={() => setEditing('')}>
                <Plus />
                Add
              </button>
            )}
          </div>
          {list.locked && <p className="au-note mc-note">This machine cannot read the keys yet. They open once another machine that can syncs.</p>}
          {editing === '' && <McpForm busy={busy === ''} onCancel={() => setEditing(null)} onSave={async (name, s) => (await act('', () => api.saveMcp(name, s))) && setEditing(null)} />}
          {list.servers.length === 0 && editing !== '' ? (
            <p className="au-note sy-empty">None yet. Add one, or move in one Claude already has.</p>
          ) : (
            <ul className="au-list">
              {list.servers.map((s) =>
                editing === s.name ? (
                  <li key={s.name} className="mc-edit">
                    <McpForm server={s} busy={busy === s.name} onCancel={() => setEditing(null)} onSave={async (name, x) => (await act(s.name, () => api.saveMcp(name, x))) && setEditing(null)} />
                  </li>
                ) : (
                  <li key={s.name} className="au-row sy-row">
                    <div className="au-main">
                      <span className="au-name au-name-static">
                        <span className="au-text">
                          <span className="au-title">{s.name}</span>
                          <span className="au-desc mc-where">{where(s)}</span>
                        </span>
                      </span>
                      <span className="sy-side">
                        <span className="sy-meta">
                          <span className="mc-state" data-state={s.state} title={s.error || (s.state === 'taken' ? `Claude already has a server named ${s.name}, set up outside Orla.` : undefined)}>
                            {STATE[s.state]}
                          </span>
                        </span>
                        <span className="sy-acts">
                          <button type="button" className="fl-act" disabled={!!busy || list.locked} onClick={() => setEditing(s.name)} aria-label={`Edit ${s.name}`}>
                            <Pencil />
                          </button>
                          <button type="button" className="fl-act" disabled={!!busy} onClick={() => void remove(s)} aria-label={`Remove ${s.name}`}>
                            <Trash />
                          </button>
                        </span>
                      </span>
                    </div>
                  </li>
                ),
              )}
            </ul>
          )}
        </section>

        {list.outside.length > 0 && (
          <section className="au-section">
            <div className="sy-head">
              <h3>Only in Claude</h3>
            </div>
            <ul className="au-list">
              {list.outside.map((s) => (
                <li key={s.name} className="au-row sy-row">
                  <div className="au-main">
                    <span className="au-name au-name-static">
                      <span className="au-text">
                        <span className="au-title">{s.name}</span>
                        <span className="au-desc mc-where">{where(s)}</span>
                      </span>
                    </span>
                    <span className="sy-side">
                      <span className="sy-meta" />
                      <span className="sy-acts">
                        <button type="button" className="fl-act" disabled={!!busy} onClick={() => void act(s.name, () => api.adoptMcp(s.name))}>
                          {busy === s.name ? 'Moving…' : 'Move in'}
                        </button>
                      </span>
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </div>
  );
}

function where(s: McpServer): string {
  return s.type === 'stdio' ? [s.command, ...(s.args ?? [])].join(' ') : (s.url ?? '');
}

/** One server: where it is, and the one key it needs, if any. A saved key is never shown. */
function McpForm({ server, busy, onSave, onCancel }: { server?: Server; busy: boolean; onSave: (name: string, s: McpInput) => Promise<unknown>; onCancel: () => void }) {
  const saved = server ? (server.type === 'stdio' ? server.env : server.headers) ?? [] : [];
  const [name, setName] = useState(server?.name ?? '');
  const [address, setAddress] = useState(server ? where(server) : '');
  const [keyName, setKeyName] = useState(saved[0] ?? 'Authorization');
  const [value, setValue] = useState('');
  const stdio = address.trim() !== '' && !/^https?:\/\//.test(address.trim());
  const id = server ? `mc-${server.name}` : 'mc-new';

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    // Other saved keys go back empty, which keeps them.
    const keys: Record<string, string> = Object.fromEntries(saved.map((k) => [k, '']));
    if (keyName.trim() && (value || saved.includes(keyName.trim()))) keys[keyName.trim()] = value;
    const a = address.trim();
    const s: McpInput = stdio
      ? (() => {
          const [command, ...args] = a.split(/\s+/);
          return { type: 'stdio', command, args, env: keys };
        })()
      : { type: server?.type === 'sse' ? 'sse' : 'http', url: a, headers: keys };
    void onSave(name.trim(), s);
  };

  return (
    <form className="mc-form" onSubmit={submit} aria-busy={busy}>
      <fieldset className="sy-fieldset" disabled={busy}>
        {!server && (
          <>
            <label className="sy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input id={`${id}-name`} className="sy-input" value={name} onChange={(e) => setName(e.target.value)} placeholder="vx-work" spellCheck={false} autoComplete="off" autoFocus />
          </>
        )}
        <label className="sy-label" htmlFor={`${id}-addr`}>
          URL or command
        </label>
        <input id={`${id}-addr`} className="sy-input mc-mono" value={address} onChange={(e) => setAddress(e.target.value)} placeholder="https://…/mcp" spellCheck={false} autoComplete="off" autoFocus={!!server} />
        <span className="sy-label">{stdio ? 'Variable' : 'Header'}</span>
        <div className="mc-key">
          <input className="sy-input mc-mono" value={keyName} onChange={(e) => setKeyName(e.target.value)} aria-label={stdio ? 'Variable name' : 'Header name'} spellCheck={false} autoComplete="off" />
          <input
            className="sy-input mc-mono"
            type="password"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            aria-label="Value"
            placeholder={saved.includes(keyName.trim()) ? 'Saved · empty keeps it' : 'Bearer …'}
            autoComplete="off"
          />
        </div>
        <div className="mc-acts">
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={!name.trim() || !address.trim()}>
            {busy ? 'Saving…' : 'Save'}
          </button>
        </div>
      </fieldset>
    </form>
  );
}

