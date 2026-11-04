'use client';

import { useState } from 'react';
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
    if (!window.confirm(`Close ${label(t)}? Every process it started ends with it.`)) return;
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
      <div className="dock-tabs" role="tablist" aria-label="Open terminals">
        {terminals.map((t) => (
          <div key={t.id} className="tab" role="tab" aria-selected={t.id === current?.id} title={`${t.argv.join(' ')} in ${shortPath(t.cwd)} · ${t.id}`}>
            <button className="tab-label" onClick={() => select(t.id)}>
              {label(t)}
            </button>
            <button className="tab-close" onClick={() => void close(t)} aria-label={`Close ${label(t)}`}>
              <Cross />
            </button>
          </div>
        ))}
        <div className="dock-add">
          <button className="btn btn-sm" onClick={() => void open()} title={cwd ? `A shell in ${shortPath(cwd)}` : 'A shell'}>
            <Plus /> Shell
          </button>
          <button className="btn btn-sm" onClick={() => void open(['claude'])} title={cwd ? `Claude Code in ${shortPath(cwd)}` : 'Claude Code'}>
            <Plus /> Claude
          </button>
        </div>
      </div>
      <div className="dock-body">
        {current ? (
          <TerminalView key={current.id} id={current.id} theme={theme} fontFamily={font} />
        ) : (
          <div className="dock-empty">
            <h2>No terminals open</h2>
            <p>
              A terminal runs on the server and keeps going when you leave it.
              {cwd ? ` New ones start in ${shortPath(cwd)}.` : ''}
            </p>
            <button className="btn btn-primary" onClick={() => void open(['claude'])}>
              <TerminalIcon /> Open Claude Code
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
