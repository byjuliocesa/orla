import '@/styles/chat.css';
import { useState, type ReactNode } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { parsePrompt } from '@/lib/prompt';
import { fenceLanguage, highlight, languageOf } from '@/lib/highlight';
import { AttachmentPreview, guessType, kindOf, type Attachment } from './Attachments';
import { between, changeCounts, clock, duration, outcomeText, toolSummary } from '@/lib/format';
import type { Approval, Limits, RunView, Step, ToolStep } from '@/lib/timeline';
import { AskActions, timeLeft } from './Ask';
import { Ban, Check, ChevronRight, Copy, Cross, FileIcon, Fork, Pin, Spinner, VideoIcon, Waiting } from './Icons';
import { Tide } from './Tide';
import { Tooltip } from './Tooltip';

interface Ctx {
  session: string;
  cwd: string;
  now: number;
  /** Steps made by events after this seq arrived live, and enter with motion. */
  liveAfter: number;
  runOpen: boolean;
  onAnswered: () => void;
  openTerminal: (id: string) => void;
  /** Forks the session from a run's answer; undefined when it cannot be. */
  fork?: (run: string) => Promise<void>;
  /** This run's chapter title, when the owner pinned it as one. */
  chapter?: string;
  toggleChapter?: (title: string) => void;
  /** A run of the session a fork came from, shown for context: no actions on it. */
  inherited?: boolean;
}

export function RunBlock({ run, ctx }: { run: RunView; limits?: Limits; ctx: Omit<Ctx, 'runOpen'> }) {
  const c: Ctx = { ...ctx, runOpen: run.status === 'running' };
  const sent = parsePrompt(run.prompt);
  const answer = run.steps.flatMap((s) => (s.kind === 'message' ? [s.text.trim()] : [])).join('\n\n');
  const chapterTitle = firstWords(sent.text);
  return (
    <section className="run" id={`run-${run.id}`} aria-label={sent.text || 'Run'} data-chapter={ctx.chapter ? '' : undefined} data-inherited={ctx.inherited || undefined}>
      {ctx.chapter && (
        <div className="chapter-mark">
          <Pin />
          <span>{ctx.chapter}</span>
        </div>
      )}
      <div className="prompt">
        {sent.files.length > 0 && <PromptFiles files={sent.files} />}
        <p className="prompt-text" data-unknown={!run.prompt}>
          {run.prompt ? sent.text : 'Prompt not seen: the hooks started reporting in the middle of it.'}
        </p>
        {!ctx.inherited && <Actions
          side="end"
          text={sent.text}
          at={run.started}
          now={ctx.now}
          chapter={ctx.chapter}
          toggleChapter={ctx.toggleChapter && (() => ctx.toggleChapter!(chapterTitle))}
          extra={
            <>
              {sent.ultracode && <span className="prompt-ultra">Ultracode</span>}
              {run.interactive && run.terminal && (
                <button className="link" onClick={() => ctx.openTerminal(run.terminal!)}>
                  in its terminal
                </button>
              )}
            </>
          }
        />}
      </div>
      {groups(run.steps).map((g, i) =>
        g.kind === 'message' ? (
          <div key={i} className="message md" data-new={g.step.seq > ctx.liveAfter}>
            <Rich text={g.step.text} />
          </div>
        ) : (
          <ToolGroup key={i} steps={g.steps} ctx={c} />
        ),
      )}
      {run.status === 'running' && <Tide since={run.started} />}
      {(run.status === 'failed' || run.status === 'cancelled') && (
        <div className="run-end" data-s={run.status}>
          {run.status === 'failed' ? <Cross /> : <Ban />}
          {run.status === 'failed' ? 'Failed' : 'Cancelled'}
          {run.summary && <span className="run-summary">{run.summary}</span>}
        </div>
      )}
      {run.status !== 'running' && answer && !ctx.inherited && <Actions side="start" text={answer} at={run.finished ?? run.started} now={ctx.now} fork={ctx.fork && (() => ctx.fork!(run.id))} />}
    </section>
  );
}

function firstWords(text: string, max = 48) {
  const line = text.trim().split('\n')[0]!.replace(/\s+/g, ' ');
  return line.length > max ? `${line.slice(0, max - 1).trimEnd()}…` : line || 'Chapter';
}

/** When it was, as people say it: just now, 3 minutes ago, yesterday. */
function relative(iso: string, now: number) {
  const s = Math.round((now - Date.parse(iso)) / 1000);
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });
  if (s < 45) return rtf.format(0, 'second');
  if (s < 3600) return rtf.format(-Math.round(s / 60), 'minute');
  if (s < 86400) return rtf.format(-Math.round(s / 3600), 'hour');
  return rtf.format(-Math.round(s / 86400), 'day');
}

/** The row under a message: copy, fork, pin as a chapter, and when, as Claude shows them. */
function Actions({
  side,
  text,
  at,
  now,
  fork,
  chapter,
  toggleChapter,
  extra,
}: {
  side: 'start' | 'end';
  text: string;
  at: string;
  now: number;
  fork?: () => Promise<void>;
  chapter?: string;
  toggleChapter?: () => void;
  extra?: ReactNode;
}) {
  const [copied, setCopied] = useState(false);
  const [forking, setForking] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {}
  };
  const full = new Date(at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
  return (
    <div className="actions" data-side={side}>
      {text && (
        <Tooltip label={copied ? 'Copied' : 'Copy'} side="bottom">
          <button type="button" className="act" onClick={() => void copy()} aria-label="Copy" data-done={copied || undefined}>
            {copied ? <Check /> : <Copy />}
          </button>
        </Tooltip>
      )}
      {fork && (
        <Tooltip label="Fork into a new session" side="bottom">
          <button
            type="button"
            className="act"
            aria-label="Fork into a new session"
            disabled={forking}
            onClick={() => {
              setForking(true);
              void fork().finally(() => setForking(false));
            }}
          >
            {forking ? <Spinner /> : <Fork />}
          </button>
        </Tooltip>
      )}
      {toggleChapter && (
        <Tooltip label={chapter ? 'Unpin chapter' : 'Pin as a chapter'} side="bottom">
          <button type="button" className="act" aria-label={chapter ? 'Unpin chapter' : 'Pin as a chapter'} aria-pressed={Boolean(chapter)} onClick={toggleChapter}>
            <Pin />
          </button>
        </Tooltip>
      )}
      {extra}
      <Tooltip label={full} side="bottom">
        <time className="act-time" dateTime={at} tabIndex={0}>
          {relative(at, now)}
        </time>
      </Tooltip>
    </div>
  );
}

// What Claude Code says reads as prose on the page; what it does stays in panels of rows.
type Group = { kind: 'message'; step: Extract<Step, { kind: 'message' }> } | { kind: 'rows'; steps: Step[] };
function groups(steps: Step[]): Group[] {
  const out: Group[] = [];
  for (const s of steps) {
    if (s.kind === 'message') out.push({ kind: 'message', step: s });
    else {
      const last = out[out.length - 1];
      if (last?.kind === 'rows') last.steps.push(s);
      else out.push({ kind: 'rows', steps: [s] });
    }
  }
  return out;
}

// What a tool did, said in a few words, as Claude's apps sum up a stretch of work.
const VERBS: Record<string, [string, string, string]> = {
  Read: ['read', 'file', 'files'],
  Edit: ['edited', 'file', 'files'],
  MultiEdit: ['edited', 'file', 'files'],
  Write: ['wrote', 'file', 'files'],
  NotebookEdit: ['edited', 'notebook', 'notebooks'],
  Bash: ['ran', 'command', 'commands'],
  BashOutput: ['read the output of', 'command', 'commands'],
  Grep: ['searched', 'time', 'times'],
  Glob: ['looked for files', '', ''],
  WebFetch: ['fetched', 'page', 'pages'],
  WebSearch: ['searched the web', '', ''],
  Task: ['ran', 'agent', 'agents'],
  Agent: ['ran', 'agent', 'agents'],
  TodoWrite: ['updated the plan', '', ''],
};

function baseName(input: unknown): string {
  const p = (input as { file_path?: string; notebook_path?: string } | null)?.file_path ?? (input as { notebook_path?: string } | null)?.notebook_path;
  return p ? p.split('/').pop()! : '';
}

function sumUp(steps: ToolStep[]): string {
  const order: string[] = [];
  const by = new Map<string, ToolStep[]>();
  for (const s of steps) {
    const key = VERBS[s.name] ? VERBS[s.name]![0] + '|' + VERBS[s.name]![1] : s.name;
    if (!by.has(key)) order.push(key);
    by.set(key, [...(by.get(key) ?? []), s]);
  }
  const parts = order.map((key) => {
    const list = by.get(key)!;
    const v = VERBS[list[0]!.name];
    if (!v) return `used ${list[0]!.name.replace(/^mcp__[^_]+__/, '')}${list.length > 1 ? ` ×${list.length}` : ''}`;
    const [verb, one, many] = v;
    if (!one) return verb;
    const name = list.length === 1 ? baseName(list[0]!.input) : '';
    return name ? `${verb} ${name}` : `${verb} ${list.length} ${list.length === 1 ? one : many}`;
  });
  const text = parts.join(', ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * A stretch of tool calls, folded into one line that says what was done, and opened on a click.
 * It opens by itself while it asks for permission, and says when something failed.
 */
function ToolGroup({ steps, ctx }: { steps: Step[]; ctx: Ctx }) {
  const tools = steps.filter((s): s is ToolStep => s.kind === 'tool');
  const asking = steps.some((s) => (s.kind === 'tool' && s.approval && !s.approval.outcome) || (s.kind === 'approval' && !s.approval.outcome));
  const running = ctx.runOpen && tools.some((s) => !s.result);
  const failed = tools.filter((s) => s.result && !s.result.ok).length;
  const [open, setOpen] = useState(false);
  const shown = open || asking;
  if (tools.length === 0) {
    return (
      <div className="steps">
        <Steps steps={steps} ctx={ctx} />
      </div>
    );
  }
  return (
    <div className="tool-group" data-open={shown || undefined}>
      <button type="button" className="tool-sum" aria-expanded={shown} onClick={() => setOpen(!open)}>
        {running ? <Spinner /> : failed ? <Cross /> : <Check />}
        <span className="tool-sum-text">{sumUp(tools)}</span>
        {failed > 0 && <span className="tool-sum-fail">{failed} failed</span>}
        <ChevronRight />
      </button>
      {shown && (
        <div className="steps">
          <Steps steps={steps} ctx={ctx} />
        </div>
      )}
    </div>
  );
}

/** The files sent with a prompt, above it: pictures for images, cards for the rest. A click opens them. */
function PromptFiles({ files }: { files: { id: string; name: string; path: string }[] }) {
  const [open, setOpen] = useState<string | null>(null);
  const items: Attachment[] = files.map((f) => {
    const type = guessType(f.name);
    return { key: f.id, name: f.name, type, size: 0, progress: 1, upload: { id: f.id, name: f.name, type, size: 0, path: f.path } };
  });
  const current = items.find((a) => a.key === open);
  return (
    <>
      <ul className="prompt-files" aria-label="Attached files">
        {items.map((a) => {
          const kind = kindOf(a);
          return (
            <li key={a.key}>
              <button type="button" className="prompt-file" data-kind={kind === 'image' ? 'image' : 'file'} onClick={() => setOpen(a.key)} aria-label={`Open ${a.name}`}>
                {kind === 'image' ? (
                  <img src={`/v1/uploads/${a.key}`} alt="" loading="lazy" />
                ) : (
                  <>
                    <span className="att-icon">{kind === 'video' ? <VideoIcon /> : <FileIcon />}</span>
                    <span className="att-name">{a.name}</span>
                  </>
                )}
              </button>
            </li>
          );
        })}
      </ul>
      {current && <AttachmentPreview items={items} current={current} go={setOpen} onClose={() => setOpen(null)} />}
    </>
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
              <div key={i} className="message md" data-new={isNew}>
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
        {edits.map((e, i) => {
          const lang = languageOf(str(o.file_path));
          return (
            <pre key={i} className="output diff">
              {e.old &&
                e.old.split('\n').map((l, j) => (
                  <span key={`o${j}`} className="del">
                    {'- '}
                    <span className="hl" dangerouslySetInnerHTML={{ __html: highlight(l, lang) }} />
                    {'\n'}
                  </span>
                ))}
              {e.neu.split('\n').map((l, j) => (
                <span key={`n${j}`} className="add">
                  {'+ '}
                  <span className="hl" dangerouslySetInnerHTML={{ __html: highlight(l, lang) }} />
                  {'\n'}
                </span>
              ))}
            </pre>
          );
        })}
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

// Claude Code writes Markdown; it is shown as such. No raw HTML, and links leave Orla's tab.
export function Rich({ text }: { text: string }) {
  return (
    <Markdown
      remarkPlugins={[remarkGfm]}
      components={{
        a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noreferrer noopener" />,
        // A fenced block is colored by its language; inline code stays as it is.
        code: ({ node: _node, className, children, ...props }) => {
          const text = String(children ?? '').replace(/\n$/, '');
          const lang = /language-([\w+-]+)/.exec(className ?? '')?.[1];
          if (!lang && !text.includes('\n')) return <code {...props}>{children}</code>;
          return <code className={className} dangerouslySetInnerHTML={{ __html: highlight(text, fenceLanguage(lang)) }} />;
        },
      }}
    >
      {text.trim()}
    </Markdown>
  );
}
