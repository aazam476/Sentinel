// SPDX-License-Identifier: Apache-2.0
import { request as playwrightRequest } from '@playwright/test';
import { type ChildProcess, spawn, spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

const binaries = resolve('..', 'bin');
const executable = (name: string) =>
  join(binaries, process.platform === 'win32' ? `${name}.exe` : name);

/**
 * OwnServer is a sentinel-server a test starts, stops and restarts itself,
 * on its own port and data directory, for flows the shared per-project
 * server can't cover (a server restart).
 */
export class OwnServer {
  readonly origin: string;
  readonly dataDir: string;
  private process: ChildProcess | undefined;

  constructor(
    private readonly port: number,
    name: string,
  ) {
    this.origin = `https://localhost:${String(port)}`;
    this.dataDir = resolve('test-results', 'e2e-data', name);
    rmSync(this.dataDir, { recursive: true, force: true });
  }

  /** start runs the server and waits until it answers. */
  async start() {
    this.process = spawn(
      executable('sentinel-server'),
      [
        '--config',
        join('e2e', 'server.toml'),
        '--data-dir',
        this.dataDir,
        '--listen',
        `127.0.0.1:${String(this.port)}`,
      ],
      { stdio: 'ignore' },
    );
    const client = await playwrightRequest.newContext({ ignoreHTTPSErrors: true });
    try {
      for (let attempt = 0; attempt < 100; attempt += 1) {
        const ok = await client
          .get(`${this.origin}/healthz`)
          .then((response) => response.ok())
          .catch(() => false);
        if (ok) {
          return;
        }
        await new Promise((resolveWait) => setTimeout(resolveWait, 100));
      }
      throw new Error('the server did not start');
    } finally {
      await client.dispose();
    }
  }

  /** stop shuts the server down gracefully and waits for it to exit. */
  async stop() {
    const running = this.process;
    this.process = undefined;
    if (running === undefined || running.exitCode !== null) {
      return;
    }
    await new Promise<void>((resolveExit) => {
      running.once('exit', () => {
        resolveExit();
      });
      running.kill('SIGTERM');
    });
  }

  setupToken(): string {
    return readFileSync(join(this.dataDir, 'setup-token'), 'utf8').trim();
  }
}

/** Agent runs the real sentinel-agent with its files in a temporary folder. */
export class Agent {
  private readonly flags: string[];
  /** root is the agent's own folder, for files a test needs it to use. */
  readonly root: string;
  private readonly configDir: string;
  private process: ChildProcess | undefined;
  /** log is everything the running agent has written to stderr. */
  log = '';

  constructor(name: string) {
    const root = resolve('test-results', 'e2e-agents', name);
    rmSync(root, { recursive: true, force: true });
    mkdirSync(root, { recursive: true });
    this.root = root;
    this.configDir = join(root, 'etc');
    this.flags = ['--state-dir', join(root, 'state'), '--config-dir', this.configDir];
  }

  /**
   * enroll runs the install command's enroll step (the agent's half of
   * install, without the operating system service).
   */
  enroll(commandFlags: string[]): string {
    const result = spawnSync(
      executable('sentinel-agent'),
      ['enroll', ...commandFlags, ...this.flags],
      {
        encoding: 'utf8',
      },
    );
    if (result.status !== 0) {
      throw new Error(`enroll failed: ${result.stderr}`);
    }
    return result.stdout;
  }

  /** writePolicy writes the agent's local policy file (SEC-22). */
  writePolicy(text: string) {
    mkdirSync(this.configDir, { recursive: true });
    writeFileSync(join(this.configDir, 'policy.toml'), text, { mode: 0o600 });
  }

  /** run starts the agent in the background, as its service would. */
  run() {
    this.process = spawn(executable('sentinel-agent'), ['run', ...this.flags], {
      stdio: ['ignore', 'ignore', 'pipe'],
    });
    this.process.stderr?.on('data', (chunk: Buffer) => {
      this.log += chunk.toString();
    });
  }

  stop() {
    this.process?.kill('SIGTERM');
  }
}

/**
 * agentFlags turns the Linux and macOS install command shown in the UI into
 * the agent's flags, dropping `sudo ./sentinel-agent install`.
 */
export function agentFlags(command: string): string[] {
  const words = command.split(/\s+/);
  const install = words.indexOf('install');
  return words.slice(install + 1).map((word) => word.replace(/^'(.*)'$/, '$1'));
}
