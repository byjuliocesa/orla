import { useState } from 'react';
import { ask } from './ConfirmDialog';
import { Tooltip } from './Tooltip';
import { api } from '@/lib/api';
import { folderName, shortPath } from '@/lib/format';
import type { TerminalInfo } from '@/lib/types';
import { ArrowLeft, Cross, Plus, TerminalIcon } from './Icons';
import { TerminalView, type TerminalTheme } from './TerminalView';

const theme: TerminalTheme = {
  background: '#050c10',
  foreground: '#dde8eb',
  cursor: '#6aa1ff',
  selectionBackground: 'rgba(106, 161, 255, 0.3)',
};

const font = "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace";

function label(t: TerminalInfo): string {
  const cmd = t.argv[0]?.split('/').pop() || 'shell';
  return `${cmd} · ${folderName(t.cwd)}`;
}

/**
 * Terminals living on the server. Switching tabs detaches and attaches again; the server keeps
 * each process and replays its screen.
 */
export function TerminalDock({
  terminals,
  active,
  cwd,
  select,
  changed,
  onBack,
}: {
  terminals: TerminalInfo[];
  active?: string;
  /** Where a new terminal starts: the open session's folder, if there is one. */
  cwd?: string;
  select: (id?: string) => void;
  changed: () => void;
  onBack: () => void;
}) {
  const [error, setError] = useState('');
  const current = terminals.find((t) => t.id === active) ?? terminals[0];

  const open = async (argv?: string[]) => {
    setError('');
    try {
      const t = await api.openTerminal({ argv, cwd });
      changed();
      select(t.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not open a terminal.');
    }
  };

  const close = async (t: TerminalInfo) => {
    if (!(await ask({ title: `Close ${label(t)}?`, body: 'Every process it started ends with it.', confirm: 'Close' }))) return;
    try {
      await api.closeTerminal(t.id);
    } finally {
      changed();
      if (t.id === current?.id) select(undefined);
    }
  };

  return (
    <aside className="dock" aria-label="Terminals">
      <div className="topbar">
        <button className="icon-btn" onClick={onBack} aria-label="Back">
          <ArrowLeft />
        </button>
        <span className="title">Terminals</span>
      </div>
      <div className="dock-tabs" role="tablist" aria-label="Open terminals" data-empty={terminals.length === 0 || undefined}>
        {terminals.map((t) => (
          <div key={t.id} className="tab" role="tab" aria-selected={t.id === current?.id}>
            <Tooltip label={`${t.argv.join(' ') || 'shell'} in ${shortPath(t.cwd)}`} side="bottom">
              <button className="tab-label" onClick={() => select(t.id)}>
                {label(t)}
              </button>
            </Tooltip>
            <button className="tab-close" onClick={() => void close(t)} aria-label={`Close ${label(t)}`}>
              <Cross />
            </button>
          </div>
        ))}
        <div className="dock-add">
          <Tooltip label={cwd ? `New shell in ${shortPath(cwd)}` : 'New shell'} side="bottom">
            <button className="icon-btn dock-new" aria-label="New shell" onClick={() => void open()}>
              <Plus />
            </button>
          </Tooltip>
        </div>
      </div>
      <div className="dock-body">
        {current ? (
          <TerminalView key={current.id} id={current.id} theme={theme} fontFamily={font} />
        ) : (
          <div className="dock-empty">
            <div className="dock-empty-term" aria-hidden="true">
              <span className="dock-empty-path">{cwd ? shortPath(cwd) : '~'}</span>
              <span className="dock-empty-prompt">❯</span>
              <span className="dock-empty-caret" />
            </div>
            <h2>No terminals yet</h2>
            <p>They run on the server and keep going after you close Orla.</p>
            <button className="btn btn-primary" onClick={() => void open()}>
              <TerminalIcon /> Open a shell
            </button>
          </div>
        )}
        {error && (
          <p className="form-error" role="alert" style={{ padding: '0 16px' }}>
            {error}
          </p>
        )}
      </div>
    </aside>
  );
}
