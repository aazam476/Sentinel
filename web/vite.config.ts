// SPDX-License-Identifier: Apache-2.0
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The Go server listens here during development (see scripts/run-dev.sh).
// The dev server forwards API and agent traffic to it so the browser talks to
// one origin, the same as in production where the Go server serves the UI.
const devBackendURL = process.env.SENTINEL_DEV_BACKEND ?? 'https://127.0.0.1:7443';

// The development server uses a self-signed certificate, so the proxy can't
// verify it. This only affects the local dev proxy, never production.
const proxyOptions = { target: devBackendURL, secure: false };

export default defineConfig({
  plugins: [react()],
  // Asset links are relative, so the built app works under any base path:
  // the server adds a <base> element pointing at the configured one.
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    // A host name rather than 127.0.0.1, because passkeys need one.
    host: 'localhost',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': proxyOptions,
      '/healthz': proxyOptions,
      '/agent': { ...proxyOptions, ws: true },
    },
  },
});
