'use client';

import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError } from '@/lib/api';
import { parsePatch, type ParsedDiff } from '@/lib/diff';
import { shortPath } from '@/lib/format';
import type { Review } from '@/lib/hooks';
import type { ChangedFile, MergeState, Session } from '@/lib/types';
import { Branch, Check, ChevronRight, Commit, Cross, Merge, Spinner, Trash, Undo } from './Icons';

const letters: Record<ChangedFile['status'], string> = {
  added: 'A',
  modified: 'M',
  deleted: 'D',
  renamed: 'R',
  untracked: 'U',
};

const statusWords: Record<ChangedFile['status'], string> = {
  added: 'Added',
  modified: 'Modified',
  deleted: 'Deleted',
  renamed: 'Renamed',
  untracked: 'New, not tracked by git yet',
};

function failure(err: unknown): string {
  return err instanceof ApiError || err instanceof Error ? err.message : 'Orla did not answer. Try again.';
}

/**
 * What the session's agent changed, file by file, the way a code review shows it: against the
 * branch its worktree came from, or against the last commit. From here the owner commits,
 * merges the worktree back, or discards a file.
 */
export function ChangesView({
  session,
  review,
  working,
  onChanged,
  onAsk,
  filesSeq,
}: {
  session: Session;
  review: Review;
  /** A run is in progress: discarding would pull files from under the agent. */
  working: boolean;
  onChanged: () => void;
  /** Sends the session's agent a prompt, as a new run. */
  onAsk: (prompt: string) => Promise<void>;
  filesSeq: number;
}) {
  const { changes, merge, error: loadError } = review;
  const [open, setOpen] = useState<Set<string> | null>(null);
  const [committing, setCommitting] = useState(false);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  const files = changes?.git ? changes.files : [];
  // A short list opens whole, as a review would; a long one starts folded.
  useEffect(() => {
    if (open === null && changes?.git) setOpen(new Set(files.length <= 5 ? files.map((f) => f.path) : []));
  }, [changes, files, open]);

  const act = async (what: string, fn: () => Promise<string>) => {
    setBusy(what);
    setError('');
    setNotice('');
    try {
      setNotice(await fn());
      review.reload();
      onChanged();
    } catch (err) {
      setError(failure(err));
    } finally {
      setBusy('');
    }
  };

  if (!changes) {
    return (
      <div className="feed">
        <p className="loading">{loadError || 'Reading the changes…'}</p>
      </div>
    );
  }

  if (!changes.git) {
    const wt = session.worktree;
    return (
      <div className="feed">
        <div className="empty">
          {wt?.removed ? (
            <>
              <h2>The worktree was removed</h2>
              <p>
                {wt.branch_kept ? (
                  <>
                    Its branch <code>{wt.branch}</code> stays in <code>{shortPath(wt.repo, 36)}</code>, since {wt.base} lacks its
                    commits.
                  </>
                ) : (
                  <>
                    Its branch <code>{wt.branch}</code> had nothing {wt.base} lacked, so it was deleted.
                  </>
                )}
              </p>
              {wt.stash && (
                <p>
                  What was not committed is in a git stash: <code className="wrap">{wt.stash}</code>
                </p>
              )}
            </>
          ) : changes.folder_gone ? (
            <>
              <h2>The session's folder is gone</h2>
              <p>
                <code>{shortPath(session.cwd, 36)}</code> no longer exists on the server.
              </p>
            </>
          ) : (
            <>
              <h2>Not a git repository</h2>
              <p>
                <code>{shortPath(session.cwd, 36)}</code> is not in a git repository, so there are no changes to review.
              </p>
            </>
          )}
        </div>
      </div>
    );
  }

  const wt = changes.worktree;
  const uncommitted = files.filter((f) => f.uncommitted).length;
  const branch = changes.repo.branch || changes.repo.head?.slice(0, 7) || 'HEAD';

  const commit = (e: FormEvent) => {
    e.preventDefault();
    const text = message.trim();
    if (!text) return;
    void act('commit', async () => {
      const r = await api.commit(session.id, text);
      setCommitting(false);
      setMessage('');
      return `Committed ${r.commit.slice(0, 7)} on ${branch}.`;
    });
  };

  const discard = (f: ChangedFile) => {
    const what = f.status === 'untracked' ? 'Delete the new file' : 'Discard the uncommitted changes to';
    if (!window.confirm(`${what} ${f.path}? Orla keeps a copy in a git stash, listed by git stash list.`)) return;
    void act(`discard:${f.path}`, async () => {
      const r = await api.discard(session.id, f.path);
      return `Discarded ${f.path}. Get it back with git stash: “${r.stash}”.`;
    });
  };

  const doMerge = (m: MergeState) => {
    if (!window.confirm(`Merge ${m.branch} into ${m.base}? Orla runs git merge in ${shortPath(m.preview.into ?? '')}.`)) return;
    void act('merge', async () => {
      await api.merge(session.id);
      return `Merged ${m.branch} into ${m.base}.`;
    });
  };

  const remove = () => {
    if (!wt) return;
    const ok = window.confirm(
      `Remove the worktree folder ${shortPath(wt.path)}? Uncommitted changes go to a git stash, and the branch ${wt.branch} stays if ${wt.base} lacks its commits.`,
    );
    if (!ok) return;
    // The view that follows says what the removal kept.
    void act('remove', async () => {
      await api.removeWorktree(session.id);
      return '';
    });
  };

  const conflicts = merge && merge.preview.ahead > 0 && !merge.preview.clean ? (merge.preview.conflicts ?? []) : [];
  const resolve = () => {
    if (!merge) return;
    const files = conflicts.length > 0 ? ` in ${conflicts.join(', ')}` : '';
    void act('ask', async () => {
      await onAsk(
        `Merge ${merge.base} into this branch (${merge.branch}) and resolve the conflicts${files}, keeping what both sides meant. Then commit the merge.`,
      );
      return '';
    });
  };

  // One forward action at a time: commit what is loose, have conflicts resolved, then merge.
  const mergeable = merge && merge.preview.ahead > 0 && merge.preview.clean && Boolean(merge.preview.into);
  const commitPrimary = uncommitted > 0;
  const resolvePrimary = !commitPrimary && conflicts.length > 0;

  return (
    <div className="feed review">
      <section className="review-bar" aria-label="Branch">
        <div className="review-what">
          <p className="review-branch">
            <span className="status">
              <Branch />
            </span>
            <span className="review-branch-text">
              <code>{branch}</code>
              {wt ? (
                <>
                  <span>from</span>
                  <code>{wt.base}</code>
                  <span className="num">
                    {changes.ahead ?? 0} {changes.ahead === 1 ? 'commit' : 'commits'} ahead
                    {changes.behind ? `, ${changes.behind} behind` : ''}
                  </span>
                </>
              ) : (
                <span>{files.length > 0 ? 'uncommitted changes' : 'nothing uncommitted'}</span>
              )}
            </span>
          </p>
          {working ? (
            <p className="review-merge">
              <span className="status" data-s="run">
                <Spinner />
              </span>
              <span>Claude Code is working; the diffs follow its edits.</span>
            </p>
          ) : (
            merge && <MergeLine merge={merge} />
          )}
        </div>
        <div className="review-actions">
          <button
            className={commitPrimary ? 'btn btn-primary' : 'btn'}
            onClick={() => setCommitting(true)}
            disabled={uncommitted === 0 || committing || Boolean(busy)}
          >
            <Commit /> Commit…
          </button>
          {conflicts.length > 0 && !working && (
            <button className={resolvePrimary ? 'btn btn-primary' : 'btn'} onClick={resolve} disabled={Boolean(busy)}>
              {busy === 'ask' ? 'Asking…' : 'Ask Claude Code to resolve'}
            </button>
          )}
          {wt && conflicts.length === 0 && (
            <button
              className={!commitPrimary && mergeable ? 'btn btn-primary' : 'btn'}
              onClick={() => merge && doMerge(merge)}
              disabled={!mergeable || Boolean(busy)}
            >
              <Merge /> {busy === 'merge' ? 'Merging…' : `Merge into ${wt.base}`}
            </button>
          )}
          {wt && (
            <button className="btn btn-remove" onClick={remove} disabled={working || Boolean(busy)} title={working ? 'Wait for the run to end' : undefined}>
              <Trash /> {busy === 'remove' ? 'Removing…' : 'Remove worktree'}
            </button>
          )}
        </div>
      </section>

      {committing && (
        <form className="commit-form" onSubmit={commit}>
          <label className="visually-hidden" htmlFor="commit-message">
            Commit message
          </label>
          <input
            id="commit-message"
            className="field"
            autoFocus
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && setCommitting(false)}
            placeholder="Commit message"
            maxLength={2000}
          />
          <button className="btn btn-primary" disabled={!message.trim() || Boolean(busy)}>
            {busy === 'commit' ? 'Committing…' : `Commit ${uncommitted} ${uncommitted === 1 ? 'file' : 'files'}`}
          </button>
          <button type="button" className="btn" onClick={() => setCommitting(false)}>
            Cancel
          </button>
        </form>
      )}

      {notice && (
        <p className="review-note" role="status">
          {notice}
        </p>
      )}
      {(error || loadError) && (
        <p className="form-error review-error" role="alert">
          {error || loadError}
        </p>
      )}

      {files.length === 0 ? (
        <div className="empty">
          <h2>No changes</h2>
          <p>{wt ? `Nothing differs from where ${wt.branch} left ${wt.base}.` : 'The work tree matches the last commit.'}</p>
        </div>
      ) : (
        <div className="steps files">
          {files.map((f) => (
            <FileDiff
              key={f.path}
              session={session.id}
              file={f}
              open={open?.has(f.path) ?? false}
              toggle={() =>
                setOpen((prev) => {
                  const next = new Set(prev ?? []);
                  if (next.has(f.path)) next.delete(f.path);
                  else next.add(f.path);
                  return next;
                })
              }
              version={filesSeq}
              conflict={conflicts.includes(f.path)}
              discard={f.uncommitted ? () => discard(f) : undefined}
              discardBlocked={working ? 'Wait for the run to end' : busy ? 'Busy' : ''}
            />
          ))}
        </div>
      )}
      {changes.more > 0 && <p className="output-note">{changes.more} more files are not listed.</p>}
    </div>
  );
}

function MergeLine({ merge }: { merge: MergeState }) {
  const p = merge.preview;
  let icon = <Check />;
  let tone = 'ok';
  let text: string;
  if (p.ahead === 0) {
    tone = 'idle';
    text = `Nothing to merge: ${merge.base} has every commit of this branch.`;
  } else if (!p.clean) {
    icon = <Cross />;
    tone = 'fail';
    text = `Conflicts with ${merge.base} in ${p.conflicts?.join(', ') || 'some files'}. Have the agent merge ${merge.base} into its branch and resolve them.`;
  } else if (!p.into) {
    tone = 'idle';
    text = `${merge.base} is not checked out anywhere, so Orla cannot merge into it.`;
  } else {
    text = `Merges cleanly into ${merge.base}.`;
  }
  return (
    <p className="review-merge" data-tone={tone}>
      <span className="status" data-s={tone}>
        {tone !== 'idle' && icon}
      </span>
      <span>
        {text}
        {merge.uncommitted > 0 &&
          ` ${merge.uncommitted} uncommitted ${merge.uncommitted === 1 ? 'file stays' : 'files stay'} out of a merge until committed.`}
      </span>
    </p>
  );
}

function FileDiff({
  session,
  file,
  open,
  toggle,
  version,
  conflict,
  discard,
  discardBlocked,
}: {
  session: string;
  file: ChangedFile;
  open: boolean;
  toggle: () => void;
  /** Merging into the base would conflict in this file. */
  conflict: boolean;
  /** Changes when the files may have changed, so an open diff reloads. */
  version: number;
  discard?: () => void;
  discardBlocked: string;
}) {
  // A nested repository is listed as a folder, "vendor/lib/".
  const path = file.path.replace(/\/$/, '');
  const slash = path.lastIndexOf('/');
  const dir = slash >= 0 ? path.slice(0, slash + 1) : '';
  const name = path.slice(slash + 1) + (path === file.path ? '' : '/');
  const bodyId = `diff-${file.path}`;

  return (
    <div className="file">
      <div className="file-head">
        <button className="step-row file-row" aria-expanded={open} aria-controls={bodyId} onClick={toggle}>
          <ChevronRight />
          <span className="file-status" data-s={file.status} title={statusWords[file.status]}>
            {letters[file.status]}
            <span className="visually-hidden">{statusWords[file.status]}</span>
          </span>
          <span className="file-path" title={file.from ? `${file.from} → ${file.path}` : file.path}>
            {dir && <span className="file-dir">{dir}</span>}
            <span className="file-name">{name}</span>
            {file.from && <span className="file-from">from {file.from}</span>}
          </span>
          <span className="file-end">
            {conflict && <span className="file-conflict">conflict</span>}
            {file.binary ? (
              <span className="file-binary">binary</span>
            ) : (
              <span className="counts num">
                {file.added > 0 && <span className="plus">+{file.added}</span>}
                {file.removed > 0 && <span className="minus">−{file.removed}</span>}
              </span>
            )}
          </span>
        </button>
        {discard && (
          <button
            className="icon-btn file-discard"
            onClick={discard}
            disabled={Boolean(discardBlocked)}
            aria-label={`Discard changes to ${file.path}`}
            title={discardBlocked || 'Discard uncommitted changes (kept in a git stash)'}
          >
            <Undo />
          </button>
        )}
      </div>
      {open && <DiffBody id={bodyId} session={session} file={file} version={version} />}
    </div>
  );
}

/** Lines drawn before "Show all": a generated file can run to tens of thousands. */
const firstLines = 1500;

function DiffBody({ id, session, file, version }: { id: string; session: string; file: ChangedFile; version: number }) {
  const [diff, setDiff] = useState<{ parsed: ParsedDiff; truncated: boolean }>();
  const [error, setError] = useState('');
  const [all, setAll] = useState(false);

  useEffect(() => {
    let stale = false;
    api
      .diff(session, file.path)
      .then((d) => {
        if (stale) return;
        setDiff({ parsed: parsePatch(d.patch), truncated: d.truncated });
        setError('');
      })
      .catch((err) => {
        // A file that stopped changing meanwhile leaves the list on the next reload.
        if (!stale && !(err instanceof ApiError && err.status === 404)) setError(failure(err));
      });
    return () => {
      stale = true;
    };
  }, [session, file.path, file.added, file.removed, version]);

  if (error) return <p className="output-note diff-note" id={id}>{error}</p>;
  if (!diff) return <p className="output-note diff-note" id={id}>Loading the diff…</p>;
  const { parsed, truncated } = diff;
  if (parsed.binary || file.binary) return <p className="output-note diff-note" id={id}>Binary file; not shown.</p>;
  if (parsed.hunks.length === 0) {
    // The row already names both paths of a rename; what is left to say is whether lines moved.
    const notes = parsed.notes.filter((n) => !n.startsWith('rename ') && !n.startsWith('similarity index'));
    const text = notes.length > 0 ? notes.join(' · ') : file.from ? 'Renamed; the content is the same.' : 'No line changes.';
    return (
      <p className="output-note diff-note" id={id}>
        {text}
      </p>
    );
  }

  const total = parsed.hunks.reduce((n, h) => n + h.lines.length + 1, 0);
  let budget = all ? Infinity : firstLines;
  return (
    <div className="diff-wrap" id={id}>
      <table className="diff">
        <tbody>
          {parsed.hunks.map((h, i) => {
            if (budget <= 0) return null;
            budget -= 1;
            const lines = h.lines.slice(0, Math.max(0, budget));
            budget -= lines.length;
            return (
              <HunkRows key={i} context={h.context} oldStart={h.oldStart} newStart={h.newStart} lines={lines} />
            );
          })}
        </tbody>
      </table>
      {!all && total > firstLines && (
        <button className="output-more" onClick={() => setAll(true)}>
          Show all {total} lines
        </button>
      )}
      {truncated && <p className="output-note">The diff is cut at 1 MB; read the rest with git diff.</p>}
    </div>
  );
}

function HunkRows({ context, oldStart, newStart, lines }: { context: string; oldStart: number; newStart: number; lines: ParsedDiff['hunks'][number]['lines'] }) {
  return (
    <>
      <tr className="hunk">
        <td className="ln" />
        <td className="ln" />
        <td className="code">
          @@ −{oldStart} +{newStart} @@{context && <span className="hunk-context"> {context}</span>}
        </td>
      </tr>
      {lines.map((l, j) => (
        <tr key={j} data-k={l.kind}>
          <td className="ln">{l.old ?? ''}</td>
          <td className="ln">{l.new ?? ''}</td>
          <td className="code">
            <span className="sign" aria-hidden="true">
              {l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}
            </span>
            {l.kind === 'note' ? <span className="diff-eof">{l.text}</span> : l.text}
          </td>
        </tr>
      ))}
    </>
  );
}
