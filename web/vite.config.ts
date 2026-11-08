import { execSync } from 'node:child_process';
import { copyFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import react from '@vitejs/plugin-react';
import type { Plugin } from 'vite';
import { defineConfig } from 'vitest/config';

// The newest git tag reachable from HEAD, like v0.1.2; "dev" without one.
function version(): string {
  try {
    return execSync('git describe --tags --abbrev=0', { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim();
  } catch {
    return 'dev';
  }
}

// The Go server serves files as they are: /login/ needs its own index.html, unknown paths get
// 404.html. Both are the same app, which picks the page from the path.
function pages(): Plugin {
  return {
    name: 'orla-pages',
    apply: 'build',
    writeBundle(opts) {
      const out = opts.dir!;
      mkdirSync(`${out}/login`, { recursive: true });
      copyFileSync(`${out}/index.html`, `${out}/login/index.html`);
      copyFileSync(`${out}/index.html`, `${out}/404.html`);
    },
  };
}

// Built as static files and embedded in the Go binary: no Node on the server.
export default defineConfig({
  plugins: [react(), pages()],
  resolve: { alias: { '@': fileURLToPath(new URL('.', import.meta.url)) } },
  define: { 'import.meta.env.VITE_ORLA_VERSION': JSON.stringify(version()) },
  // Every asset a file of its own: the server's CSP allows fonts and images from 'self', not data: URIs.
  build: { outDir: 'out', emptyOutDir: true, assetsInlineLimit: 0 },
  server: { host: '127.0.0.1', port: 3000, strictPort: true },
  test: { include: ['lib/**/*.test.ts'] },
});
