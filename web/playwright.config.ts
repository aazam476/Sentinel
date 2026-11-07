// SPDX-License-Identifier: Apache-2.0
import { defineConfig, devices } from '@playwright/test';

// The end-to-end tests drive the real server binary, which embeds the built
// web UI. `make test-e2e` builds both before running Playwright.
const serverBinary = process.platform === 'win32' ? 'sentinel-server.exe' : 'sentinel-server';
const serverAddress = '127.0.0.1:7480';

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
    baseURL: `http://${serverAddress}`,
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
    command: `../bin/${serverBinary} --listen ${serverAddress}`,
    url: `http://${serverAddress}/`,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
