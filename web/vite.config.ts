// SPDX-License-Identifier: Apache-2.0
import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

// The Go server listens here during development (see scripts/run-dev.sh).
// The dev server forwards API and agent traffic to it so the browser talks to
// one origin, the same as in production where the Go server serves the UI.
const devBackendURL = process.env.SENTINEL_DEV_BACKEND ?? 'https://127.0.0.1:7443';

// The development server uses a self-signed certificate, so the proxy can't
// verify it. This only affects the local dev proxy, never production.
const proxyOptions = { target: devBackendURL, secure: false };

/**
 * Sonner adds its stylesheet to the page as an inline <style> element, which
 * the server's Content-Security-Policy (style-src 'self') blocks. In the
 * production build this removes that call; the app imports the same styles
 * from sonner/dist/styles.css instead, as a normal CSS file. The build fails
 * if a Sonner upgrade changes the code this looks for, so the problem can't
 * come back silently. (The dev server doesn't send the CSP, so it doesn't
 * need this.)
 */
function sonnerWithoutInlineStyles(): Plugin {
  return {
    name: 'sentinel:sonner-without-inline-styles',
    apply: 'build',
    transform(code, id) {
      if (!/[\\/]node_modules[\\/]sonner[\\/]dist[\\/]index\.mjs$/.test(id)) {
        return null;
      }
      const patched = code.replace(/^__insertCSS\(".*"\);$/m, '');
      if (patched === code) {
        this.error('Sonner no longer injects styles the way this plugin expects; update it.');
      }
      return { code: patched, map: null };
    },
  };
}

/**
 * xterm.js styles the terminal by adding <style> elements and, for 24-bit
 * colors, `style` attributes, both of which the Content-Security-Policy
 * blocks. In the production build this points the <style> elements at
 * <sentinel-style> (src/components/Terminal/sentinelStyle.ts), which applies
 * the same CSS through a constructed stylesheet, and sets colors through
 * the CSSOM (`style.cssText`), which the policy allows. Like the Sonner
 * plugin, it fails the build if an upgrade changes the code it patches.
 */
function xtermWithoutInlineStyles(): Plugin {
  const replacements: [search: string, replace: string, count: number][] = [
    ['createElement("style")', 'createElement("sentinel-style")', 3],
    [
      't.setAttribute("style",`${t.getAttribute("style")||""}${e};`)',
      't.style.cssText+=`${e};`',
      1,
    ],
  ];
  return {
    name: 'sentinel:xterm-without-inline-styles',
    apply: 'build',
    transform(code, id) {
      if (!/[\\/]node_modules[\\/]@xterm[\\/]xterm[\\/]lib[\\/]xterm\.mjs$/.test(id)) {
        return null;
      }
      let patched = code;
      for (const [search, replace, count] of replacements) {
        const found = patched.split(search).length - 1;
        if (found !== count) {
          this.error(`xterm.js no longer matches what this plugin patches (${search}); update it.`);
        }
        patched = patched.replaceAll(search, replace);
      }
      return { code: patched, map: null };
    },
  };
}

export default defineConfig({
  plugins: [react(), sonnerWithoutInlineStyles(), xtermWithoutInlineStyles()],
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
