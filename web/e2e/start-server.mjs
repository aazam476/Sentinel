// SPDX-License-Identifier: Apache-2.0
//
// Starts sentinel-server for the end-to-end tests with a fresh, empty data
// directory, so every run begins with a brand-new installation.
//
// Usage (from web/): node e2e/start-server.mjs <listen address> <name>
//
// The data directory is test-results/e2e-data/<name>, where the tests read
// the setup token from.

import { spawn } from 'node:child_process';
import { rmSync } from 'node:fs';
import { join } from 'node:path';

const listenAddress = process.argv[2] ?? '127.0.0.1:7480';
const dataDir = join('test-results', 'e2e-data', process.argv[3] ?? 'default');
const binary = join(
  '..',
  'bin',
  process.platform === 'win32' ? 'sentinel-server.exe' : 'sentinel-server',
);

rmSync(dataDir, { recursive: true, force: true });

const server = spawn(
  binary,
  ['--config', join('e2e', 'server.toml'), '--data-dir', dataDir, '--listen', listenAddress],
  { stdio: 'inherit' },
);

// Playwright stops this script when the tests finish; pass that on.
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.kill(signal));
}
server.on('exit', (code) => process.exit(code ?? 1));
