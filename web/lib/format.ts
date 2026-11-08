// How numbers, times and tool inputs read in the interface.

export function ago(iso: string | undefined, now: number): string {
  if (!iso) return '';
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 45) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  return d === 1 ? 'yesterday' : `${d} days ago`;
}

/** A clock reading for something in progress: 0:53, 4:10, 1:02:03. */
export function clock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

/** A finished step's duration: 0.1s, 27s, 1m 12s. */
export function duration(ms: number): string {
  if (ms < 0) return '';
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)}s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
}

export function between(from: string, to: string | undefined): number {
  return to ? Date.parse(to) - Date.parse(from) : -1;
}

export function tokens(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}

export function cost(usd: number): string {
  return usd >= 0.01 ? `$${usd.toFixed(2)}` : `$${usd.toFixed(4)}`;
}

/** The folder a session works in, by its last name: ~/work/orla reads "orla". */
export function folderName(cwd: string): string {
  const parts = cwd.replace(/\/+$/, '').split('/');
  return parts[parts.length - 1] || cwd;
}

/** What a session is called in lists: its folder, or its repository and worktree branch. */
/** The home directory of a user on the server: where a new session starts. */
export function isHome(path: string): boolean {
  return /^\/(home\/[^/]+|root|Users\/[^/]+)\/?$/.test(path);
}

function promptName(text: string, max = 60): string {
  const line = text.trim().split('\n')[0]!.replace(/\s+/g, ' ');
  return line.length > max ? `${line.slice(0, max - 1).trimEnd()}…` : line || 'New session';
}

export function sessionName(s: { cwd: string; title?: string; worktree?: { repo: string; branch: string }; last_run?: { prompt: string; status: string } }): string {
  if (s.title) return s.title;
  // A session started in the home directory waits a few seconds for its name; if none comes
  // by the end of its first run, its prompt stands in.
  if (!s.worktree && isHome(s.cwd)) return s.last_run && s.last_run.status !== 'running' ? promptName(s.last_run.prompt) : 'New session';
  if (!s.worktree) return folderName(s.cwd);
  return `${folderName(s.worktree.repo)} · ${s.worktree.branch.replace(/^orla\//, '')}`;
}

/** A path as people read it: ~ for home, and the middle cut out when it is long. */
export function shortPath(path: string, max = 48): string {
  const p = path.replace(/^\/home\/[^/]+/, '~').replace(/^\/Users\/[^/]+/, '~');
  if (p.length <= max) return p;
  const tail = Math.round(max * 0.6);
  return `${p.slice(0, max - tail - 1)}…${p.slice(-tail)}`;
}

/**
 * The input field that says the most about a tool call. Paths inside the session's folder read
 * relative to it, the way the agent was asked about them.
 */
export function toolSummary(input: unknown, cwd?: string): string {
  if (input && typeof input === 'object') {
    const o = input as Record<string, unknown>;
    for (const k of ['command', 'file_path', 'path', 'pattern', 'url', 'query', 'description', 'prompt']) {
      const v = o[k];
      if (typeof v !== 'string' || !v) continue;
      return firstLine(cwd ? relative(v, cwd) : v);
    }
    const json = JSON.stringify(input);
    return json === '{}' ? '' : firstLine(json);
  }
  return typeof input === 'string' ? firstLine(input) : '';
}

/** Lines an edit removes and adds, for Edit, MultiEdit and Write calls. */
export function changeCounts(name: string, input: unknown): { minus: number; plus: number } | null {
  if (!input || typeof input !== 'object') return null;
  const o = input as Record<string, unknown>;
  const lines = (v: unknown) => (typeof v === 'string' && v !== '' ? v.replace(/\n$/, '').split('\n').length : 0);
  if (name === 'Edit') return { minus: lines(o.old_string), plus: lines(o.new_string) };
  if (name === 'Write') return { minus: 0, plus: lines(o.content) };
  if (name === 'MultiEdit' && Array.isArray(o.edits)) {
    return o.edits.reduce(
      (c: { minus: number; plus: number }, e: Record<string, unknown>) => ({ minus: c.minus + lines(e.old_string), plus: c.plus + lines(e.new_string) }),
      { minus: 0, plus: 0 },
    );
  }
  return null;
}

/** Model ids as people say them: claude-haiku-5-5 reads "haiku 5.5". */
export function modelName(id: string | undefined): string {
  if (!id) return '';
  const m = /^claude-([a-z]+)-(\d+)-(\d+)/.exec(id);
  return m ? `${m[1]} ${m[2]}.${m[3]}` : id;
}

/** Text with the session folder's paths made relative: the folder itself reads ".". */
function relative(s: string, cwd: string): string {
  const dir = cwd.replace(/\/+$/, '');
  const escaped = dir.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return s.replaceAll(`${dir}/`, '').replace(new RegExp(`${escaped}(?=$|[\\s'"])`, 'g'), '.');
}

export function firstLine(s: string): string {
  const t = s.trim();
  const i = t.indexOf('\n');
  return i < 0 ? t : `${t.slice(0, i)} …`;
}

// Endings no person decided: Orla gave up waiting, the run ended, or the CLI stopped asking.
const systemEndings = new Set(['timeout', 'cancel', 'run']);

/** What an approval outcome says, in a few words, with the reason or the owner's note. */
export function outcomeText(o: { allowed: boolean; by: string; message?: string; withdrawn: boolean }): string {
  if (o.withdrawn) return o.message ? `No longer asking here: ${o.message}` : 'No longer asking here';
  const verdict = o.allowed ? 'Allowed' : 'Denied';
  if (systemEndings.has(o.by)) return o.message ? `${verdict}: ${o.message}` : verdict;
  const where = o.by === 'terminal' ? ' in the terminal' : o.by ? ` from ${o.by}` : '';
  return `${verdict}${where}${!o.allowed && o.message ? `: ${o.message}` : ''}`;
}

/** Claude Code's permission modes, as a run can request them. */
export const permissionModes: { value: string; label: string }[] = [
  { value: '', label: 'Agent settings' },
  { value: 'manual', label: 'Ask before each tool' },
  { value: 'acceptEdits', label: 'Accept edits' },
  { value: 'plan', label: 'Plan only' },
  { value: 'auto', label: 'Auto' },
  { value: 'bypassPermissions', label: 'Bypass permissions' },
];
