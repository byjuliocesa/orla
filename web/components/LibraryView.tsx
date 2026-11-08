import '@/styles/automations.css';
import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { layout } from '@/lib/flowlayout';
import type { Flow, Session } from '@/lib/types';
import { AutomationsView } from './AutomationsView';
import { ask } from './ConfirmDialog';
import { Archive, ArrowLeft, Bolt, ChevronRight, Copy, FlowIcon, Trash } from './Icons';
import { toast } from './Toast';

type Tab = 'flows' | 'automations';
const TAB_KEY = 'orla.library.tab';

/**
 * The Library: everything the owner keeps for every session. Flows, which sessions choose to
 * follow, and automations, which run with a session or on their own.
 */
export function LibraryView({ sessions, onBack }: { sessions: Session[]; onBack: () => void }) {
  const [tab, setTabState] = useState<Tab>(() => {
    try {
      return localStorage.getItem(TAB_KEY) === 'automations' ? 'automations' : 'flows';
    } catch {
      return 'flows';
    }
  });
  const setTab = (t: Tab) => {
    setTabState(t);
    try {
      localStorage.setItem(TAB_KEY, t);
    } catch {}
  };
  return (
    <div className="lib">
      <header className="lib-headbar">
        <div className="lib-top">
          <button className="icon-btn lib-back" onClick={onBack} aria-label="Back">
            <ArrowLeft />
          </button>
          <h1 className="lib-h1">Library</h1>
        </div>
        <div className="head-main">
          <div className="head-tabs" role="group" aria-label="Show">
            <button className="head-tab" aria-pressed={tab === 'flows'} onClick={() => setTab('flows')}>
              <FlowIcon />
              Flows
            </button>
            <button className="head-tab" aria-pressed={tab === 'automations'} onClick={() => setTab('automations')}>
              <Bolt />
              Automations
            </button>
          </div>
        </div>
      </header>
      <div className="tab-stage" key={tab} data-from={tab === 'flows' ? 'left' : 'right'}>
        {tab === 'flows' ? <FlowLibrary sessions={sessions} /> : <AutomationsView sessions={sessions} />}
      </div>
    </div>
  );
}

function FlowLibrary({ sessions }: { sessions: Session[] }) {
  const [flows, setFlows] = useState<Flow[] | null>(null);
  const [archived, setArchived] = useState<Flow[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let stale = false;
    api.flows('').then((r) => !stale && setFlows(r.flows), () => !stale && setFlows([]));
    api.archivedFlows().then((r) => !stale && setArchived(r.flows), () => {});
    return () => {
      stale = true;
    };
  }, [tick]);

  const act = async (action: 'archive' | 'restore' | 'delete' | 'duplicate', f: Flow) => {
    if (action === 'delete' && !(await ask({ title: `Delete ${f.title}?`, body: `${f.title} is removed for good. Archive it instead to keep it.`, confirm: 'Delete', danger: true }))) return;
    if (action === 'archive' && !(await ask({ title: `Archive ${f.title}?`, body: 'No session follows it any more; it waits in the archive.', confirm: 'Archive', danger: false }))) return;
    try {
      await api.flowAction(action, f.name, undefined, '');
      toast(`${f.title} ${{ archive: 'archived', restore: 'restored', delete: 'deleted', duplicate: 'duplicated' }[action]}.`, 'info');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Orla could not change the flow.');
    }
    setTick((t) => t + 1);
  };

  if (!flows) return <div className="au-wrap" role="status" aria-label="Reading the flows" />;
  const followers = (f: Flow) => sessions.filter((s) => s.flows?.includes(f.name));

  return (
    <div className="au-wrap">
      <div className="au-col">
        <div className="au-head">
          <div>
            <h2>Flows</h2>
            <p>How each kind of task is done. A session follows the ones it picks, in its Flow tab.</p>
          </div>
        </div>
        {flows.length === 0 ? (
          <p className="au-note">No flows yet. Create one from a session’s Flow tab, or with /newflow.</p>
        ) : (
          <ul className="au-list">
            {flows.map((f) => {
              const by = followers(f);
              const lay = layout(f);
              return (
                <li key={f.name} className="au-row" data-open={open === f.name || undefined}>
                  <div className="au-main">
                    <button type="button" className="au-name" aria-expanded={open === f.name} onClick={() => setOpen(open === f.name ? null : f.name)}>
                      <ChevronRight />
                      <span className="au-text">
                        <span className="au-title">{f.title}</span>
                        <span className="au-desc">{f.description}</span>
                      </span>
                    </button>
                    <span className="au-trigger num">{f.steps.length} steps</span>
                    <span className="au-count">{by.length === 0 ? 'No session' : by.length === 1 ? '1 session' : `${by.length} sessions`}</span>
                  </div>
                  {open === f.name && (
                    <div className="au-detail">
                      <ol className="lib-steps">
                        {f.steps.map((s) => (
                          <li key={s.id} data-side={(lay.cells.get(s.id)?.col ?? 0) > 0 || undefined}>
                            <span className="lib-step">{s.title}</span>
                            {s.branches.map((b, i) => (
                              <span key={i} className="lib-branch">
                                ↳ {f.steps.find((x) => x.id === b.to)?.title ?? b.to}
                                {b.when ? ` ${b.when}` : ''}
                              </span>
                            ))}
                          </li>
                        ))}
                      </ol>
                      {by.length > 0 && <p className="au-note">Followed in {by.map((s) => s.title || s.cwd).join(', ')}.</p>}
                      <div className="fl-actions">
                        <button type="button" className="fl-act" onClick={() => void act('duplicate', f)}>
                          <Copy />
                          Duplicate
                        </button>
                        <button type="button" className="fl-act fl-act-archive" onClick={() => void act('archive', f)}>
                          <Archive />
                          Archive
                        </button>
                        <button type="button" className="fl-act fl-act-delete" onClick={() => void act('delete', f)}>
                          <Trash />
                          Delete
                        </button>
                      </div>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
        {archived.length > 0 && (
          <section className="au-section">
            <h3>Archived</h3>
            <ul className="au-list">
              {archived.map((f) => (
                <li key={f.name} className="au-row">
                  <div className="au-main">
                    <span className="au-name au-name-static">
                      <span className="au-text">
                        <span className="au-title">{f.title}</span>
                        <span className="au-desc">{f.steps.length} steps</span>
                      </span>
                    </span>
                    <button type="button" className="fl-restore" onClick={() => void act('restore', f)}>
                      Restore
                    </button>
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
