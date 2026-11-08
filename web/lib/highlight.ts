import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import c from 'highlight.js/lib/languages/c';
import cpp from 'highlight.js/lib/languages/cpp';
import csharp from 'highlight.js/lib/languages/csharp';
import css from 'highlight.js/lib/languages/css';
import diff from 'highlight.js/lib/languages/diff';
import dockerfile from 'highlight.js/lib/languages/dockerfile';
import go from 'highlight.js/lib/languages/go';
import ini from 'highlight.js/lib/languages/ini';
import java from 'highlight.js/lib/languages/java';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import kotlin from 'highlight.js/lib/languages/kotlin';
import makefile from 'highlight.js/lib/languages/makefile';
import markdown from 'highlight.js/lib/languages/markdown';
import php from 'highlight.js/lib/languages/php';
import python from 'highlight.js/lib/languages/python';
import ruby from 'highlight.js/lib/languages/ruby';
import rust from 'highlight.js/lib/languages/rust';
import scss from 'highlight.js/lib/languages/scss';
import sql from 'highlight.js/lib/languages/sql';
import swift from 'highlight.js/lib/languages/swift';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';

// The languages a session's code is likely in; others show as plain text.
const LANGS = { bash, c, cpp, csharp, css, diff, dockerfile, go, ini, java, javascript, json, kotlin, makefile, markdown, php, python, ruby, rust, scss, sql, swift, typescript, xml, yaml };
for (const [name, lang] of Object.entries(LANGS)) hljs.registerLanguage(name, lang);

const BY_EXT: Record<string, string> = {
  sh: 'bash', bash: 'bash', zsh: 'bash', fish: 'bash',
  c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cs: 'csharp',
  css: 'css', scss: 'scss', diff: 'diff', patch: 'diff',
  go: 'go', mod: 'go', ini: 'ini', toml: 'ini', env: 'ini',
  java: 'java', kt: 'kotlin', js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', tsx: 'typescript', json: 'json', md: 'markdown', php: 'php', py: 'python',
  rb: 'ruby', rs: 'rust', sql: 'sql', swift: 'swift', html: 'xml', htm: 'xml', xml: 'xml', svg: 'xml',
  vue: 'xml', yml: 'yaml', yaml: 'yaml',
};

/** The language of a file, from its name; undefined when Orla does not color it. */
export function languageOf(path: string): string | undefined {
  const name = path.split('/').pop()!.toLowerCase();
  if (name === 'makefile') return 'makefile';
  if (name === 'dockerfile') return 'dockerfile';
  return BY_EXT[name.split('.').pop()!];
}

/** A language a Markdown fence names, as highlight.js knows it. */
export function fenceLanguage(name: string | undefined): string | undefined {
  if (!name) return undefined;
  const n = name.toLowerCase();
  if (hljs.getLanguage(n)) return n;
  return BY_EXT[n];
}

/**
 * Code as HTML with its tokens in spans. highlight.js escapes the text itself, so the result is
 * safe to set as HTML. Unknown languages come back escaped, uncolored.
 */
export function highlight(code: string, language: string | undefined): string {
  if (language && hljs.getLanguage(language)) {
    try {
      return hljs.highlight(code, { language, ignoreIllegals: true }).value;
    } catch {
      // fall through to plain
    }
  }
  return code.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
