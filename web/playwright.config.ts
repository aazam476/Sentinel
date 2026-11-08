// SPDX-License-Identifier: Apache-2.0
import { defineConfig, devices } from '@playwright/test';

// The end-to-end tests drive the real server binary, which embeds the built
// web UI. `make test-e2e` builds both before running Playwright.
//
// Setting up the first admin can only happen once per server, so each
// project below gets its own server with a fresh data directory. Browsers
// only allow passkeys on a host name, so the URLs use localhost.
const projects = [
  { name: 'desktop', port: 7480, reducedMotion: false, phone: false },
  { name: 'phone', port: 7481, reducedMotion: false, phone: true },
  { name: 'desktop-reduced-motion', port: 7482, reducedMotion: true, phone: false },
  { name: 'phone-reduced-motion', port: 7483, reducedMotion: true, phone: true },
];

const desktop = { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } };
// iPhone-sized, but on Chromium so CI needs only one browser.
const phone = {
  ...devices['Desktop Chrome'],
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  deviceScaleFactor: 3,
};

export default defineConfig({
  testDir: './e2e',
  forbidOnly: process.env.CI !== undefined,
  retries: 0,
  // One worker per project: tests within a project share its server.
  workers: projects.length,
  fullyParallel: false,
  reporter: process.env.CI !== undefined ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    // A fresh server uses a self-signed certificate, like a real first install.
    ignoreHTTPSErrors: true,
    trace: 'off',
  },
  // Every flow runs at desktop and phone width, each with and without
  // reduced motion, so a flow that only works with animations on is caught.
  projects: projects.map((project) => ({
    name: project.name,
    use: {
      ...(project.phone ? phone : desktop),
      baseURL: `https://localhost:${String(project.port)}`,
      contextOptions: { reducedMotion: project.reducedMotion ? 'reduce' : 'no-preference' },
    },
  })),
  webServer: projects.map((project) => ({
    command: `node e2e/start-server.mjs 127.0.0.1:${String(project.port)} ${project.name}`,
    url: `https://localhost:${String(project.port)}/healthz`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    stdout: 'ignore' as const,
    stderr: 'pipe' as const,
  })),
});
