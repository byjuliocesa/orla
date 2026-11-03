// Reads a unified diff, as `git diff` prints it, into hunks of numbered lines.

export type LineKind = 'context' | 'add' | 'del' | 'note';

export interface DiffLine {
  kind: LineKind;
  text: string;
  /** Line number in the old file; absent on added lines. */
  old?: number;
  /** Line number in the new file; absent on removed lines. */
  new?: number;
}

export interface Hunk {
  /** What git prints after the second @@: usually the enclosing function. */
  context: string;
  oldStart: number;
  newStart: number;
  lines: DiffLine[];
}

export interface ParsedDiff {
  hunks: Hunk[];
  binary: boolean;
  /** Mode changes, renames: the header lines that say something beyond the content. */
  notes: string[];
}

const hunkHead = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/;

export function parsePatch(patch: string): ParsedDiff {
  const out: ParsedDiff = { hunks: [], binary: false, notes: [] };
  let hunk: Hunk | undefined;
  let oldNo = 0;
  let newNo = 0;
  const lines = patch.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  for (const line of lines) {
    const head = hunkHead.exec(line);
    if (head) {
      oldNo = Number(head[1]);
      newNo = Number(head[2]);
      hunk = { context: head[3] ?? '', oldStart: oldNo, newStart: newNo, lines: [] };
      out.hunks.push(hunk);
      continue;
    }
    if (!hunk) {
      if (line.startsWith('Binary files ')) out.binary = true;
      else if (/^(old mode|new mode|deleted file mode|new file mode|rename from|rename to|similarity index)/.test(line))
        out.notes.push(line);
      continue;
    }
    switch (line[0]) {
      case '+':
        hunk.lines.push({ kind: 'add', text: line.slice(1), new: newNo++ });
        break;
      case '-':
        hunk.lines.push({ kind: 'del', text: line.slice(1), old: oldNo++ });
        break;
      case ' ':
        hunk.lines.push({ kind: 'context', text: line.slice(1), old: oldNo++, new: newNo++ });
        break;
      case '\\':
        // "\ No newline at end of file"
        hunk.lines.push({ kind: 'note', text: line.slice(2) });
        break;
      default:
        // The next file's header in a multi-file patch: this hunk is over.
        if (line.startsWith('diff --git')) hunk = undefined;
    }
  }
  return out;
}
