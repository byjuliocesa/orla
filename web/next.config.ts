import type { NextConfig } from 'next';

// Exported as static files and embedded in the Go binary: no Node on the server.
const config: NextConfig = {
  output: 'export',
  trailingSlash: true,
  images: { unoptimized: true },
  reactStrictMode: true,
  poweredByHeader: false,
};

export default config;
