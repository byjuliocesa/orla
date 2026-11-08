import { useCallback, useEffect, useRef, useState } from 'react';
import { api, type PlanLimits } from '@/lib/api';
import { Tooltip } from './Tooltip';

const NAMES: Record<string, string> = { five_hour: '5-hour limit', seven_day: 'Weekly · all models', seven_day_opus: 'Weekly · Opus', seven_day_sonnet: 'Weekly · Sonnet' };
const ORDER = ['five_hour', 'seven_day', 'seven_day_opus', 'seven_day_sonnet'];

function resets(at: number | undefined, name: string): string {
  if (!at) return '';
  const d = new Date(at * 1000);
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return name === 'five_hour' ? `Resets at ${time}` : `Resets ${d.toLocaleDateString([], { weekday: 'short' })} ${time}`;
}

/**
 * How much of the plan is used, as Claude Code last reported it: a ring for the weekly window,
 * and on a click every window with its reset. Only runs report it, so it is as of the last run.
 */
export function UsageRing({ refreshKey }: { refreshKey?: unknown }) {
  const [plan, setPlan] = useState<PlanLimits | null>(null);
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);

  const load = useCallback(() => {
    api.limits().then(setPlan, () => {});
  }, []);
  useEffect(load, [load, refreshKey]);
  useEffect(() => {
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  }, [load]);

  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => !root.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    addEventListener('pointerdown', away);
    addEventListener('keydown', esc);
    return () => {
      removeEventListener('pointerdown', away);
      removeEventListener('keydown', esc);
    };
  }, [open]);

  const windows = Object.entries(plan?.limits ?? {}).sort(([a], [b]) => ORDER.indexOf(a) - ORDER.indexOf(b));
  if (windows.length === 0) return null;
  const week = plan!.limits!.seven_day ?? windows[0]![1];
  const weekName = plan!.limits!.seven_day ? 'seven_day' : windows[0]![0];
  const pct = Math.round(week.used * 100);
  const tone = pct >= 90 ? 'fail' : pct >= 75 ? 'wait' : 'ok';
  const r = 7;
  const c = 2 * Math.PI * r;

  return (
    <div className="usage" ref={root}>
      <Tooltip
        side="top"
        off={open}
        label={
          <span className="usage-tip">
            <span>
              {NAMES[weekName] ?? weekName}: {pct}%
            </span>
            <span className="usage-tip-dim">{resets(week.resets_at, weekName)}</span>
          </span>
        }
      >
        <button type="button" className="usage-ring" data-tone={tone} aria-label={`Plan usage: ${pct}% of the week`} aria-expanded={open} onClick={() => setOpen(!open)}>
          <svg viewBox="0 0 18 18" width="18" height="18" aria-hidden="true">
            <circle cx="9" cy="9" r={r} className="usage-track" />
            <circle cx="9" cy="9" r={r} className="usage-fill" strokeDasharray={`${(c * Math.min(1, week.used)).toFixed(2)} ${c.toFixed(2)}`} />
          </svg>
        </button>
      </Tooltip>
      {open && (
        <div className="usage-pop" role="dialog" aria-label="Plan usage limits">
          <div className="usage-head">
            <span>Plan usage limits</span>
          </div>
          {windows.map(([name, w]) => {
            const p = Math.round(w.used * 100);
            return (
              <div key={name} className="usage-row" data-tone={p >= 90 ? 'fail' : p >= 75 ? 'wait' : 'ok'}>
                <div className="usage-line">
                  <span>{NAMES[name] ?? name.replace(/_/g, ' ')}</span>
                  <span className="usage-dim">
                    {resets(w.resets_at, name)} <b>{p}%</b>
                  </span>
                </div>
                <span className="usage-bar">
                  <span style={{ transform: `scaleX(${Math.min(1, w.used)})` }} />
                </span>
              </div>
            );
          })}
          {plan?.at && <p className="usage-note">As of the last run, {new Date(plan.at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</p>}
        </div>
      )}
    </div>
  );
}
