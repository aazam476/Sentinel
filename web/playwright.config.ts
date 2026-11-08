// SPDX-License-Identifier: Apache-2.0
import { defineConfig, devices } from '@playwright/test';

// The end-to-end tests drive the real server binary, which embeds the built
// web UI. `make test-e2e` builds both before running Playwright.
const serverAddress = '127.0.0.1:7480';
// Browsers only allow passkeys on a host name, never an IP address.
const serverURL = 'https://localhost:7480';

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
  reporter: process.env.CI !== undefined ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: serverURL,
    // A fresh server uses a self-signed certificate, like a real first install.
    ignoreHTTPSErrors: true,
    trace: 'off',
  },
  // Every flow runs at desktop and phone width, each with and without
  // reduced motion, so a flow that only works with animations on is caught.
  projects: [
    { name: 'desktop', use: desktop },
    { name: 'phone', use: phone },
    {
      name: 'desktop-reduced-motion',
      use: { ...desktop, contextOptions: { reducedMotion: 'reduce' } },
    },
    {
      name: 'phone-reduced-motion',
      use: { ...phone, contextOptions: { reducedMotion: 'reduce' } },
    },
  ],
  webServer: {
    command: `node e2e/start-server.mjs ${serverAddress}`,
    url: `${serverURL}/healthz`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
