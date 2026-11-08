import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react';
import { api } from '@/lib/api';
import type { Session } from '@/lib/types';
import { ChevronRight, Cross, Grip, Pencil, Plus } from './Icons';
import { toast } from './Toast';
import { Tooltip } from './Tooltip';

type Skill = { name: string; description?: string; scope: string; tokens: number };

/** How the owner arranged the skills: groups in order, each with its skills in order. */
type Group = { id: string; title: string; custom?: boolean; collapsed?: boolean; skills: string[] };
type Layout = { groups: Group[] };

const LAYOUT_KEY = 'skills.layout';
const LAYOUT_CACHE = 'orla.skills.layout';
const LIBRARY_CACHE = (cwd: string) => `orla.skills.${cwd}`;

// Where a skill comes from, as Claude Code says it: the group it sits in until the owner moves it.
const SOURCES: { id: string; title: string; match: (scope: string) => boolean }[] = [
  { id: 'user', title: 'Yours', match: (s) => s === 'user' },
  { id: 'project', title: 'This project', match: (s) => s === 'project' },
  { id: 'plugin', title: 'Plugins', match: (s) => s.startsWith('plugin') },
  { id: 'sync', title: 'From claude.ai', match: (s) => s.includes('claude.ai') },
  { id: 'built-in', title: 'Built in', match: (s) => s === 'built-in' },
];
const sourceOf = (scope: string) => {
  const known = SOURCES.find((g) => g.match(scope));
  return known ? { id: known.id, title: known.title } : { id: scope, title: scope.charAt(0).toUpperCase() + scope.slice(1) };
};

const shortName = (name: string) => name.replace(/^anthropic-skills:/, '');
const tokens = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n));

function readCache<T>(key: string): T | null {
  try {
    const v = localStorage.getItem(key);
    return v ? (JSON.parse(v) as T) : null;
  } catch {
    return null;
  }
}
function writeCache(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {}
}

/**
 * The arrangement the page shows: the owner's groups in their order, then every skill not placed
 * yet in the group of where it comes from. Groups of origin that end up empty are left out; the
 * owner's own groups stay, empty or not, to drop skills into.
 */
function arrange(skills: Skill[], layout: Layout | null): Group[] {
  const byName = new Map(skills.map((s) => [s.name, s]));
  const placed = new Set<string>();
  const groups: Group[] = (layout?.groups ?? []).map((g) => {
    const list = g.skills.filter((n) => byName.has(n) && !placed.has(n));
    list.forEach((n) => placed.add(n));
    return { ...g, skills: list };
  });
  const rank = (id: string) => SOURCES.findIndex((x) => x.id === id);
  for (const s of skills) {
    if (placed.has(s.name)) continue;
    const src = sourceOf(s.scope);
    let g = groups.find((x) => x.id === src.id);
    if (!g) {
      g = { id: src.id, title: src.title, skills: [] };
      // Sources keep their usual order among themselves when the owner has not moved them.
      const at = rank(src.id) < 0 ? -1 : groups.findIndex((x) => !x.custom && rank(x.id) > rank(src.id));
      if (at >= 0) groups.splice(at, 0, g);
      else groups.push(g);
    }
    g.skills.push(s.name);
  }
  return groups.filter((g) => g.custom || g.skills.length > 0);
}

type Drag = { kind: 'skill'; name: string } | { kind: 'group'; id: string };
type Drop = { kind: 'skill'; group: string; index: number } | { kind: 'group'; index: number };

/**
 * Every skill Claude Code has, for all sessions; here, which this session keeps. A skill turned
 * off leaves the session's context altogether, from its next message on. Groups, their order and
 * which are folded belong to the owner and follow them to every browser.
 */
export function SkillsView({ session, onChanged }: { session: Session; onChanged: () => void }) {
  const [skills, setSkills] = useState<Skill[] | null>(() => readCache<Skill[]>(LIBRARY_CACHE(session.cwd)));
  const [layout, setLayout] = useState<Layout | null>(() => readCache<Layout>(LAYOUT_CACHE));
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [off, setOff] = useState<Set<string>>(() => new Set(session.skills_off ?? []));
  const [renaming, setRenaming] = useState<string | null>(null);
  const [drop, setDrop] = useState<Drop | null>(null);
  const drag = useRef<Drag | null>(null);

  useEffect(() => setOff(new Set(session.skills_off ?? [])), [session.skills_off]);
  useEffect(() => {
    let stale = false;
    setError('');
    api.skills(session.cwd).then(
      (r) => {
        if (stale) return;
        // Flows have their own tab.
        const list = r.skills.filter((s) => !s.flow && !s.orla);
        setSkills(list);
        writeCache(LIBRARY_CACHE(session.cwd), list);
      },
      (err) => !stale && setError(err instanceof Error ? err.message : 'Claude Code did not list its skills.'),
    );
    api.pref<Layout>(LAYOUT_KEY).then(
      (r) => {
        if (stale || !r.value) return;
        setLayout(r.value);
        writeCache(LAYOUT_CACHE, r.value);
      },
      () => {},
    );
    return () => {
      stale = true;
    };
  }, [session.cwd]);

  const groups = useMemo(() => (skills ? arrange(skills, layout) : []), [skills, layout]);
  const byName = useMemo(() => new Map((skills ?? []).map((s) => [s.name, s])), [skills]);

  // Every change to the arrangement is kept whole, so the next visit sees exactly this page.
  const keep = (next: Group[]) => {
    const value: Layout = { groups: next };
    setLayout(value);
    writeCache(LAYOUT_CACHE, value);
    api.setPref(LAYOUT_KEY, value).catch((err) => toast(err instanceof Error ? err.message : 'The arrangement did not reach Orla.'));
  };
  const patch = (id: string, change: Partial<Group>) => keep(groups.map((g) => (g.id === id ? { ...g, ...change } : g)));

  const save = async (next: Set<string>) => {
    const before = off;
    setOff(next);
    try {
      await api.updateSession(session.id, { skills_off: [...next].sort() });
      onChanged();
      if (session.active_run) toast('Applies from the next message; the run in progress keeps what it had.', 'info');
    } catch (err) {
      setOff(before);
      toast(err instanceof Error ? err.message : 'The change did not reach Orla.');
    }
  };
  const toggle = (name: string) => {
    const next = new Set(off);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    void save(next);
  };
  const setAll = (names: string[], on: boolean) => {
    const next = new Set(off);
    for (const n of names) {
      if (on) next.delete(n);
      else next.add(n);
    }
    void save(next);
  };

  const addGroup = () => {
    const id = `g${Date.now().toString(36)}`;
    keep([{ id, title: 'New group', custom: true, skills: [] }, ...groups]);
    setRenaming(id);
  };
  const removeGroup = (id: string) => keep(groups.filter((g) => g.id !== id));

  const q = query.trim().toLowerCase();
  const matches = (s: Skill) => !q || s.name.toLowerCase().includes(q) || !!s.description?.toLowerCase().includes(q);
  const sorting = !q; // moving things while a search hides some of them would put them in odd places

  // Dragging: a skill drops between skills or at the end of a group; a group drops between groups.
  const begin = (e: DragEvent, d: Drag) => {
    drag.current = d;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', d.kind === 'skill' ? d.name : d.id);
    e.stopPropagation();
  };
  const end = () => {
    drag.current = null;
    setDrop(null);
  };
  const half = (e: DragEvent) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    return e.clientY > r.top + r.height / 2 ? 1 : 0;
  };
  const setDropIf = (d: Drop) => {
    if (JSON.stringify(d) !== JSON.stringify(drop)) setDrop(d);
  };
  const overRow = (e: DragEvent, group: string, index: number) => {
    if (drag.current?.kind !== 'skill') return;
    e.preventDefault();
    e.stopPropagation();
    setDropIf({ kind: 'skill', group, index: index + half(e) });
  };
  const overGroup = (e: DragEvent, gi: number, head: boolean) => {
    const d = drag.current;
    if (!d) return;
    e.preventDefault();
    e.stopPropagation();
    if (d.kind === 'skill') {
      const g = groups[gi];
      if (!g) return;
      setDropIf({ kind: 'skill', group: g.id, index: head && !g.collapsed ? 0 : g.skills.length });
      return;
    }
    setDropIf({ kind: 'group', index: gi + half(e) });
  };
  const land = (e: DragEvent) => {
    e.preventDefault();
    const d = drag.current;
    const to = drop;
    end();
    if (!d || !to) return;
    if (d.kind === 'skill' && to.kind === 'skill') {
      const next = groups.map((g) => ({ ...g, skills: [...g.skills] }));
      const from = next.find((g) => g.skills.includes(d.name));
      const target = next.find((g) => g.id === to.group);
      if (!from || !target) return;
      let index = to.index;
      const was = from.skills.indexOf(d.name);
      if (from === target && was < index) index--;
      if (from === target && was === index) return;
      from.skills.splice(was, 1);
      target.skills.splice(index, 0, d.name);
      keep(next);
    } else if (d.kind === 'group' && to.kind === 'group') {
      const was = groups.findIndex((g) => g.id === d.id);
      let index = to.index;
      if (was < index) index--;
      if (was === index) return;
      const next = [...groups];
      const [moved] = next.splice(was, 1);
      if (!moved) return;
      next.splice(index, 0, moved);
      keep(next);
    }
  };

  if (error && !skills) {
    return (
      <div className="sk-scroll">
        <div className="sk-col">
          <p className="sk-empty">{error}</p>
        </div>
      </div>
    );
  }
  if (!skills) return <SkillsSkeleton />;

  const on = skills.filter((s) => !off.has(s.name));
  const onTokens = on.reduce((n, s) => n + s.tokens, 0);
  const allTokens = skills.reduce((n, s) => n + s.tokens, 0);
  const anyShown = skills.some(matches);

  return (
    <div className="sk-scroll" onDragOver={(e) => drag.current && e.preventDefault()} onDrop={land}>
      <div className="sk-col">
        <div className="sk-head">
          <div className="sk-sum">
            <span className="sk-big num">
              {on.length} of {skills.length} on
            </span>
            <span className="sk-dim num">
              {tokens(onTokens)} of {tokens(allTokens)} tokens in every message’s context
            </span>
          </div>
          <div className="sk-tools">
            <input className="sk-search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search skills" aria-label="Search skills" spellCheck={false} />
            <Tooltip label="New group" side="bottom">
              <button type="button" className="icon-btn sk-new" aria-label="New group" onClick={addGroup}>
                <Plus />
              </button>
            </Tooltip>
          </div>
        </div>

        {groups.map((g, gi) => {
          const list = g.skills.map((n) => byName.get(n)!).filter(matches);
          if (q && list.length === 0) return null;
          const open = !g.collapsed || !!q;
          const allOn = list.length > 0 && list.every((s) => !off.has(s.name));
          const offHere = g.skills.filter((n) => off.has(n)).length;
          const groupMark = drop?.kind === 'group' ? (drop.index === gi ? 'before' : drop.index === gi + 1 && gi === groups.length - 1 ? 'after' : undefined) : undefined;
          const intoFolded = drop?.kind === 'skill' && drop.group === g.id && !open;
          return (
            <section key={g.id} className="sk-group" aria-label={g.title} data-drop={groupMark} onDragOver={(e) => overGroup(e, gi, false)}>
              <div
                className="sk-group-head"
                data-target={intoFolded || undefined}
                draggable={sorting && renaming !== g.id}
                onDragStart={(e) => begin(e, { kind: 'group', id: g.id })}
                onDragEnd={end}
                onDragOver={(e) => overGroup(e, gi, true)}
              >
                {sorting && (
                  <span className="sk-grip" aria-hidden="true">
                    <Grip />
                  </span>
                )}
                <button type="button" className="sk-fold" aria-expanded={open} aria-label={open ? `Fold ${g.title}` : `Unfold ${g.title}`} onClick={() => patch(g.id, { collapsed: !g.collapsed })} disabled={!!q}>
                  <ChevronRight />
                </button>
                {renaming === g.id ? (
                  <input
                    className="sk-rename"
                    defaultValue={g.title}
                    autoFocus
                    aria-label="Group name"
                    onFocus={(e) => e.currentTarget.select()}
                    onBlur={(e) => {
                      const title = e.currentTarget.value.trim();
                      setRenaming(null);
                      if (title && title !== g.title) patch(g.id, { title });
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') e.currentTarget.blur();
                      if (e.key === 'Escape') {
                        e.currentTarget.value = g.title;
                        e.currentTarget.blur();
                      }
                    }}
                  />
                ) : (
                  <h3 className="sk-title" onClick={() => !q && patch(g.id, { collapsed: !g.collapsed })} onDoubleClick={() => setRenaming(g.id)}>
                    {g.title}
                  </h3>
                )}
                <span className="sk-count num">
                  {g.skills.length}
                  {offHere > 0 && <> · {offHere} off</>}
                </span>
                <span className="sk-group-tools">
                  <Tooltip label="Rename" side="bottom">
                    <button type="button" className="icon-btn sk-icon" aria-label={`Rename ${g.title}`} onClick={() => setRenaming(g.id)}>
                      <Pencil />
                    </button>
                  </Tooltip>
                  {g.custom && (
                    <Tooltip label="Remove group; its skills go back where they came from" side="bottom">
                      <button type="button" className="icon-btn sk-icon" aria-label={`Remove ${g.title}`} onClick={() => removeGroup(g.id)}>
                        <Cross />
                      </button>
                    </Tooltip>
                  )}
                  {list.length > 0 && (
                    <button type="button" className="sk-text-btn" onClick={() => setAll(list.map((s) => s.name), !allOn)}>
                      {allOn ? 'Turn all off' : 'Turn all on'}
                    </button>
                  )}
                </span>
              </div>

              <div className="sk-fold-body" data-open={open || undefined}>
                <div className="sk-fold-inner">
                  <ul className="sk-list">
                    {list.map((s, i) => {
                      const isOn = !off.has(s.name);
                      const mark = drop?.kind === 'skill' && drop.group === g.id ? (drop.index === i ? 'before' : drop.index === i + 1 && i === list.length - 1 ? 'after' : undefined) : undefined;
                      return (
                        <li
                          key={s.name}
                          className="sk-row"
                          data-on={isOn || undefined}
                          data-drop={mark}
                          draggable={sorting}
                          onDragStart={(e) => begin(e, { kind: 'skill', name: s.name })}
                          onDragEnd={end}
                          onDragOver={(e) => overRow(e, g.id, i)}
                        >
                          {sorting && (
                            <span className="sk-grip" aria-hidden="true">
                              <Grip />
                            </span>
                          )}
                          <div className="sk-text">
                            <span className="sk-name">{shortName(s.name)}</span>
                            {s.description && <span className="sk-desc">{s.description}</span>}
                          </div>
                          <Tooltip label="What it takes in the context of every message while it is on" side="top">
                            <span className="sk-tokens num">{tokens(s.tokens)} tokens</span>
                          </Tooltip>
                          <button
                            type="button"
                            role="switch"
                            aria-checked={isOn}
                            aria-label={`${shortName(s.name)} ${isOn ? 'on' : 'off'}`}
                            className="sk-switch"
                            onClick={() => toggle(s.name)}
                          />
                        </li>
                      );
                    })}
                    {list.length === 0 && (
                      <li className="sk-drop-here" data-drop={drop?.kind === 'skill' && drop.group === g.id ? 'before' : undefined}>
                        Drag skills here
                      </li>
                    )}
                  </ul>
                </div>
              </div>
            </section>
          );
        })}
        {!anyShown && <p className="sk-empty">No skill matches “{query}”.</p>}
      </div>
    </div>
  );
}

/** The page's own shape while Claude Code is asked for its skills, the first time only. */
function SkillsSkeleton() {
  return (
    <div className="sk-scroll" role="status" aria-label="Asking Claude Code for its skills">
      <div className="sk-col sk-skel">
        <div className="sk-head">
          <div className="sk-sum">
            <b style={{ width: 150, height: 18 }} />
            <b style={{ width: 230, height: 10 }} />
          </div>
        </div>
        {[3, 6].map((rows, g) => (
          <div key={g} className="sk-group">
            <div className="sk-group-head">
              <b style={{ width: 90, height: 10 }} />
            </div>
            {Array.from({ length: rows }, (_, i) => (
              <div key={i} className="sk-row" style={{ animationDelay: `${(g * 3 + i) * 70}ms` }}>
                <div className="sk-text">
                  <b style={{ width: `${30 + ((i * 37 + g * 11) % 35)}%`, height: 10 }} />
                  {g === 0 && <b style={{ width: `${60 + ((i * 13) % 25)}%`, height: 8 }} />}
                </div>
                <b style={{ width: 52, height: 8 }} />
                <b style={{ width: 32, height: 18, borderRadius: 9 }} />
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
