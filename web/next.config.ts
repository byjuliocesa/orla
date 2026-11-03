import { execSync } from 'node:child_process';
import type { NextConfig } from 'next';

// The newest git tag, or the commit when there is none yet; "dev" outside a git checkout.
function version(): string {
  try {
    return execSync('git describe --tags --always', { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim();
  } catch {
    return 'dev';
  }
}

// Exported as static files and embedded in the Go binary: no Node on the server.
const config: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: { unoptimized: true },
  reactStrictMode: true,
  poweredByHeader: false,
  env: { NEXT_PUBLIC_ORLA_VERSION: version() },
  agentRules: false,
  // The README's dev setup opens the client at 127.0.0.1, which Next.js blocks by default.
  allowedDevOrigins: ['127.0.0.1'],
  // Bottom left is where the login page shows the version.
  devIndicators: { position: 'bottom-right' },
};

export default config;
