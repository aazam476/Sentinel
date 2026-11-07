// SPDX-License-Identifier: Apache-2.0
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The Go server listens here during development (see scripts/run-dev.sh).
// The dev server forwards API and agent traffic to it so the browser talks to
// one origin, the same as in production where the Go server serves the UI.
const devBackendURL = process.env.SENTINEL_DEV_BACKEND ?? 'http://127.0.0.1:7443';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': devBackendURL,
      '/healthz': devBackendURL,
      '/agent': { target: devBackendURL, ws: true },
    },
  },
});
