import { useState, type ReactNode } from 'react';
import { between, changeCounts, clock, cost, duration, outcomeText, toolSummary, tokens } from '@/lib/format';
import type { Approval, Limits, RunView, Step, ToolStep } from '@/lib/timeline';
import { AskActions, timeLeft } from './Ask';
import { Ban, Check, ChevronRight, Cross, Spinner, Waiting } from './Icons';

interface Ctx {
  session: string;
  cwd: string;
  now: number;
  /** Steps made by events after this seq arrived live, and enter with motion. */
  liveAfter: number;
  runOpen: boolean;
  onAnswered: () => void;
  openTerminal: (id: string) => void;
}

export function RunBlock({ run, limits, ctx }: { run: RunView; limits?: Limits; ctx: Omit<Ctx, 'runOpen'> }) {
  const c: Ctx = { ...ctx, runOpen: run.status === 'running' };
  return (
    <section className="run" aria-label={run.prompt || 'Run'}>
      <div className="prompt">
        <div className="prompt-meta">
          <time dateTime={run.started}>{new Date(run.started).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</time>
          {run.interactive && run.terminal ? (
            <span>
              typed in{' '}
              <button className="link" onClick={() => ctx.openTerminal(run.terminal!)}>
                {run.terminal.slice(0, 13)}
              </button>
            </span>
          ) : (
            <span>sent from Orla</span>
          )}
          {run.permissionMode && <span>{run.permissionMode}</span>}
        </div>
        <p className="prompt-text" data-unknown={!run.prompt}>
          {run.prompt || 'Prompt not seen: the hooks started reporting in the middle of it.'}
        </p>
      </div>
      {run.steps.length > 0 && (
        <div className="steps">
          <Steps steps={run.steps} ctx={c} />
        </div>
      )}
      <RunFoot run={run} limits={limits} now={ctx.now} />
    </section>
  );
}

function Steps({ steps, ctx }: { steps: Step[]; ctx: Ctx }) {
  return (
    <>
      {steps.map((s, i) => {
        const isNew = s.seq > ctx.liveAfter;
        switch (s.kind) {
          case 'tool':
            return <ToolStepView key={s.callId || i} step={s} ctx={ctx} isNew={isNew} />;
          case 'message':
            return (
              <div key={i} className="message" data-new={isNew}>
                <Rich text={s.text} />
              </div>
            );
          case 'approval':
            return (
              <div key={s.approval.id} className="step" data-new={isNew}>
                <AskBlock name={s.approval.tool} approval={s.approval} ctx={ctx} solo />
              </div>
            );
          case 'error':
            return (
              <div key={i} className="error-row" role="alert">
                <Cross />
                {s.message}
              </div>
            );
        }
      })}
    </>
  );
}

type Status = 'ok' | 'fail' | 'wait' | 'run' | 'denied' | 'none';

function statusOf(step: ToolStep, runOpen: boolean): Status {
  const a = step.approval;
  if (a && !a.outcome) return 'wait';
  if (a?.outcome && !a.outcome.allowed && !a.outcome.withdrawn) return 'denied';
  if (step.result) return step.result.ok ? 'ok' : 'fail';
  return runOpen ? 'run' : 'none';
}

const statusIcon: Record<Status, ReactNode> = {
  ok: <Check />,
  fail: <Cross />,
  wait: <Waiting />,
  run: <Spinner />,
  denied: <Ban />,
  none: null,
};

const statusLabel: Record<Status, string> = {
  ok: 'succeeded',
  fail: 'failed',
  wait: 'waiting for your permission',
  run: 'running',
  denied: 'denied',
  none: 'no result',
};

// Commands show what they printed; reads and searches stay folded until asked for.
const openByDefault = new Set(['Bash', 'BashOutput']);

function ToolStepView({ step, ctx, isNew }: { step: ToolStep; ctx: Ctx; isNew: boolean }) {
  const status = statusOf(step, ctx.runOpen);
  const [open, setOpen] = useState(status === 'fail' || (openByDefault.has(step.name) && Boolean(step.result?.output)));
  const summary = toolSummary(step.input, ctx.cwd);
  const counts = changeCounts(step.name, step.input);
  const time = step.result
    ? duration(between(step.at, step.result.at))
    : status === 'run' || status === 'wait'
      ? clock(ctx.now - Date.parse(step.at))
      : '';
  const bodyId = `body-${step.callId}`;
  const asking = Boolean(step.approval && !step.approval.outcome);

  return (
    <div className="step" data-new={isNew}>
      <button className="step-row" aria-expanded={open} aria-controls={bodyId} onClick={() => setOpen(!open)}>
        <ChevronRight />
        <span className="status" data-s={status}>
          {statusIcon[status]}
          <span className="visually-hidden">{statusLabel[status]}</span>
        </span>
        <span className="step-line">
          <span className="step-tool">{step.name || 'Tool'}</span>
          {summary && <code className="step-summary">{summary}</code>}
          {counts && (counts.plus > 0 || counts.minus > 0) && (
            <span className="counts num">
              {counts.plus > 0 && <span className="plus">+{counts.plus}</span>}
              {counts.minus > 0 && <span className="minus">−{counts.minus}</span>}
            </span>
          )}
        </span>
        <span className="step-time">{time}</span>
      </button>
      {step.approval && <AskBlock name={step.name} approval={step.approval} ctx={ctx} />}
      {open && (
        <div className="step-body" id={bodyId}>
          {!asking && <InputPreview name={step.name} input={step.input} cwd={ctx.cwd} named />}
          {step.result ? (
            <Output text={step.result.output} failed={!step.result.ok} truncated={step.result.truncated} interrupted={step.result.interrupted} />
          ) : (
            <p className="output-note">{status === 'run' ? 'Running…' : asking ? 'Waits for your answer.' : 'No result was reported.'}</p>
          )}
          {step.children.length > 0 && (
            <div className="steps children">
              <Steps steps={step.children} ctx={ctx} />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

const tailLines = 6;

/** A step's output. Long output shows its end first, where commands say how they went. */
function Output({ text, failed, truncated, interrupted }: { text: string; failed: boolean; truncated: boolean; interrupted: boolean }) {
  const [all, setAll] = useState(false);
  if (!text) return <p className="output-note">No output.</p>;
  const lines = text.replace(/\n+$/, '').split('\n');
  const cut = !all && lines.length > tailLines;
  return (
    <>
      {cut && (
        <button className="output-more" onClick={() => setAll(true)}>
          Show all {lines.length} lines
        </button>
      )}
      <pre className="output" data-fail={failed}>
        {cut ? lines.slice(-tailLines).join('\n') : text}
      </pre>
      {truncated && <p className="output-note">Cut at 8 KB; the full output is in the agent's own transcript.</p>}
      {interrupted && <p className="output-note">Interrupted.</p>}
    </>
  );
}

/**
 * What a tool call will do, in full: the command, or the file and the change. Shown before an
 * answer is asked for, so nothing is approved on a summary: the boxes scroll, nothing is cut.
 * named: the caller already shows the file, so the preview does not repeat it.
 */
export function InputPreview({ name, input, cwd, named = false }: { name: string; input: unknown; cwd?: string; named?: boolean }) {
  if (!input || typeof input !== 'object') return null;
  const o = input as Record<string, unknown>;
  const str = (v: unknown) => (typeof v === 'string' ? v : '');
  const rel = (p: unknown) => toolSummary({ file_path: str(p) }, cwd);

  if (typeof o.command === 'string') {
    return (
      <div className="preview">
        {str(o.description) && <p className="preview-note">{str(o.description)}</p>}
        <pre className="output cmd">{o.command}</pre>
      </div>
    );
  }
  if (name === 'Edit' || name === 'MultiEdit' || name === 'Write') {
    const edits: { old: string; neu: string }[] =
      name === 'Write'
        ? [{ old: '', neu: str(o.content) }]
        : name === 'Edit'
          ? [{ old: str(o.old_string), neu: str(o.new_string) }]
          : Array.isArray(o.edits)
            ? o.edits.map((e: Record<string, unknown>) => ({ old: str(e.old_string), neu: str(e.new_string) }))
            : [];
    return (
      <div className="preview">
        {!named && (
          <p className="preview-note">
            <code>{rel(o.file_path)}</code>
          </p>
        )}
        {edits.map((e, i) => (
          <pre key={i} className="output diff">
            {e.old && e.old.split('\n').map((l, j) => <span key={`o${j}`} className="del">{`- ${l}\n`}</span>)}
            {e.neu.split('\n').map((l, j) => (
              <span key={`n${j}`} className="add">{`+ ${l}\n`}</span>
            ))}
          </pre>
        ))}
      </div>
    );
  }
  const rows = Object.entries(o).filter(([, v]) => v !== undefined && v !== null && v !== '');
  if (rows.length === 0) return null;
  return (
    <dl className="detail">
      {rows.slice(0, 8).map(([k, v]) => (
        <div key={k} style={{ display: 'contents' }}>
          <dt>{k.replaceAll('_', ' ')}</dt>
          <dd>
            <code>{typeof v === 'string' ? v : JSON.stringify(v, null, 2)}</code>
          </dd>
        </div>
      ))}
    </dl>
  );
}

function AskBlock({ name, approval, ctx, solo = false }: { name: string; approval: Approval; ctx: Ctx; solo?: boolean }) {
  const o = approval.outcome;
  if (o) {
    return (
      <p className="outcome" data-allowed={o.allowed || o.withdrawn}>
        {outcomeText(o)}
      </p>
    );
  }
  return (
    <div className={solo ? 'ask ask-solo' : 'ask'} role="group" aria-label={`Permission for ${approval.tool}`}>
      <p className="ask-head">
        {solo && (
          <>
            <strong>{approval.tool}</strong>{' '}
          </>
        )}
        Waiting for your permission
        {approval.terminal && ' · also asked in the terminal'}
        {approval.expiresAt && ` · ${timeLeft(approval.expiresAt, ctx.now)}`}
      </p>
      <InputPreview name={name} input={approval.input} cwd={ctx.cwd} named={!solo} />
      <AskActions session={ctx.session} approval={approval.id} onAnswered={ctx.onAnswered} />
    </div>
  );
}

function RunFoot({ run, limits, now }: { run: RunView; limits?: Limits; now: number }) {
  const parts: string[] = [];
  const u = run.usage;
  if (u?.turns) parts.push(`${u.turns} ${u.turns === 1 ? 'turn' : 'turns'}`);
  const ms = u?.durationMs ?? (run.finished ? between(run.started, run.finished) : now - Date.parse(run.started));
  if (ms > 0) parts.push(run.status === 'running' ? clock(ms) : duration(ms));
  const t = u ?? run.live;
  const total = t.input + t.output + t.cacheRead + t.cacheWrite;
  if (total > 0) parts.push(`${tokens(total)} tokens`);
  if (u?.costUsd !== undefined) parts.push(`≈${cost(u.costUsd)} at API prices`);
  if (limits && run.status !== 'running') {
    const windows = Object.entries(limits)
      .map(([name, w]) => `${name.replace('_', ' ')} ${Math.round(w.used * 100)}%`)
      .join(', ');
    if (windows) parts.push(`plan used: ${windows}`);
  }

  const verdict: Record<string, ReactNode> = {
    running: (
      <>
        <Spinner /> Running
      </>
    ),
    done: (
      <>
        <Check /> Done
      </>
    ),
    failed: (
      <>
        <Cross /> Failed
      </>
    ),
    cancelled: (
      <>
        <Ban /> Cancelled
      </>
    ),
  };

  return (
    <div className="run-foot">
      <span className="verdict" data-s={run.status}>
        {verdict[run.status] ?? run.status}
      </span>
      {parts.map((p) => (
        <span key={p}>{p}</span>
      ))}
      {run.summary && run.status !== 'done' && <span className="run-summary">{run.summary}</span>}
    </div>
  );
}

/** Plain text with `inline code` marked as code. */
function Rich({ text }: { text: string }) {
  const parts = text.trim().split(/(`[^`\n]+`)/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith('`') && p.endsWith('`') && p.length > 2 ? <code key={i}>{p.slice(1, -1)}</code> : <span key={i}>{p}</span>,
      )}
    </>
  );
}
