/** What Orla added to a prompt it sent, read back so the owner sees what they wrote. */
export interface SentPrompt {
  text: string;
  files: { id: string; name: string; path: string }[];
  ultracode: boolean;
}

const FILES = /\n\nAttached files:((?:\n- [^\n]+)+)/;
const UPLOAD = /\/(upl_[0-9a-f]{24})\/([^/]+)$/;

// The server appends the attachments' paths, then the ultracode keyword (internal/server).
export function parsePrompt(prompt: string): SentPrompt {
  let text = prompt;
  let ultracode = false;
  if (/\n\nultracode\s*$/.test(text)) {
    ultracode = true;
    text = text.replace(/\n\nultracode\s*$/, '');
  }
  const files: SentPrompt['files'] = [];
  const m = FILES.exec(text);
  if (m && m.index + m[0].length === text.length) {
    for (const line of m[1]!.split('\n').slice(1)) {
      const path = line.slice(2);
      const u = UPLOAD.exec(path);
      if (u) files.push({ id: u[1]!, name: u[2]!, path });
    }
    if (files.length) text = text.slice(0, m.index);
  }
  return { text, files, ultracode };
}
