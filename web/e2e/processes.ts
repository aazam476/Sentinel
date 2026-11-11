// SPDX-License-Identifier: Apache-2.0
import { request as playwrightRequest } from '@playwright/test';
import { type ChildProcess, spawn, spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
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
}

/** Agent runs the real sentinel-agent with its files in a temporary folder. */
export class Agent {
  private readonly flags: string[];
  /** root is the agent's own folder, for files a test needs it to use. */
  readonly root: string;
  private readonly configDir: string;
  /** binary is the agent's own copy, which updates replace. */
  readonly binary: string;
  private process: ChildProcess | undefined;
  private stopped = false;
  /** log is everything the running agent has written to stderr. */
  log = '';
  /** restarts counts the times it exited to restart, as after an update. */
  restarts = 0;

  constructor(name: string) {
    const root = resolve('test-results', 'e2e-agents', name);
    rmSync(root, { recursive: true, force: true });
    mkdirSync(root, { recursive: true });
    this.root = root;
    this.configDir = join(root, 'etc');
    this.flags = ['--state-dir', join(root, 'state'), '--config-dir', this.configDir];
    // Its own copy, as an installed agent has: an update replaces it.
    mkdirSync(join(root, 'bin'), { recursive: true });
    this.binary = join(
      root,
      'bin',
      process.platform === 'win32' ? 'sentinel-agent.exe' : 'sentinel-agent',
    );
    copyFileSync(executable('sentinel-agent'), this.binary);
    chmodSync(this.binary, 0o755);
  }

  /**
   * enroll runs the install command's enroll step (the agent's half of
   * install, without the operating system service).
   */
  enroll(commandFlags: string[]): string {
    const result = spawnSync(this.binary, ['enroll', ...commandFlags, ...this.flags], {
      encoding: 'utf8',
    });
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

  /**
   * run starts the agent in the background, as its service would, and
   * starts it again when it exits to restart (exit code 3, after an update).
   */
  run() {
    this.process = spawn(this.binary, ['run', ...this.flags], {
      stdio: ['ignore', 'ignore', 'pipe'],
    });
    this.process.stderr?.on('data', (chunk: Buffer) => {
      this.log += chunk.toString();
    });
    this.process.on('exit', (code) => {
      if (code === 3 && !this.stopped) {
        this.restarts++;
        this.run();
      }
    });
  }

  stop() {
    this.stopped = true;
    this.process?.kill('SIGTERM');
  }
}

/**
 * XDisplay is a virtual X display (Xvfb) for remote desktop on a real X
 * server, with xprobe (internal/testutil/xprobe) to paint on it and read
 * the pointer.
 */
export class XDisplay {
  private server: ChildProcess | undefined;
  private probe = '';
  /** name is the display, ":N", once started. */
  name = '';

  /** start runs Xvfb, which says its display number once it is ready. */
  async start(folder: string) {
    mkdirSync(folder, { recursive: true });
    this.probe = resolve(folder, 'xprobe');
    const built = spawnSync('go', ['build', '-o', this.probe, './internal/testutil/xprobe'], {
      cwd: resolve('..'),
      encoding: 'utf8',
      env: { ...process.env, CGO_ENABLED: '0' },
    });
    if (built.status !== 0) {
      throw new Error(`building xprobe failed: ${built.stderr}`);
    }
    const server = spawn(
      'Xvfb',
      ['-displayfd', '3', '-screen', '0', '800x600x24', '-wr', '-noreset', '-nolisten', 'tcp'],
      {
        stdio: ['ignore', 'ignore', 'ignore', 'pipe'],
      },
    );
    this.server = server;
    const number = await new Promise<string>((done, fail) => {
      let text = '';
      server.stdio[3]?.on('data', (chunk: Buffer) => {
        text += chunk.toString();
        if (text.includes('\n')) {
          done(text.trim());
        }
      });
      server.on('exit', (code) => {
        fail(new Error(`Xvfb exited with ${String(code)}`));
      });
    });
    this.name = `:${number}`;
  }

  /** paint fills a red square at (100, 100), 60 pixels wide. */
  paint() {
    this.runProbe('paint');
  }

  /** pointer is where the pointer is, and the X button mask. */
  pointer(): { x: number; y: number; buttons: number } {
    const [x, y, buttons] = this.runProbe('pointer').trim().split(' ').map(Number);
    return { x, y, buttons };
  }

  stop() {
    this.server?.kill('SIGTERM');
  }

  private runProbe(command: string): string {
    const result = spawnSync(this.probe, [this.name, command], { encoding: 'utf8' });
    if (result.status !== 0) {
      throw new Error(`xprobe ${command} failed: ${result.stderr}`);
    }
    return result.stdout;
  }
}

/**
 * agentFlags turns the Linux and macOS install command shown in the UI into
 * the agent's flags.
 */
export function agentFlags(command: string): string[] {
  const words = command.split(/\s+/);
  // The script command passes its flags after `sh -s --`; the program's
  // own command after `install`.
  const start = words.includes('--') ? words.indexOf('--') : words.indexOf('install');
  return words.slice(start + 1).map((word) => word.replace(/^'(.*)'$/, '$1'));
}

/**
 * signedAgentBuild builds an agent of a chosen version for this machine (or,
 * with broken, a program that fails at once) and signs it as a release with
 * the development key, which test builds trust. It returns the build's and
 * its signature's paths.
 */
export function signedAgentBuild(folder: string, version: string, broken = false) {
  const repository = resolve('..');
  const goos = process.platform === 'win32' ? 'windows' : process.platform;
  const goarch = process.arch === 'x64' ? 'amd64' : process.arch;
  const name = `sentinel-agent_${version}_${goos}_${goarch}${goos === 'windows' ? '.exe' : ''}`;
  mkdirSync(folder, { recursive: true });
  const output = join(folder, name);
  const source = broken ? './internal/testutil/brokenagent' : './cmd/sentinel-agent';
  const run = (command: string, args: string[], env: Record<string, string> = {}) => {
    const result = spawnSync(command, args, {
      cwd: repository,
      encoding: 'utf8',
      env: { ...process.env, CGO_ENABLED: '0', ...env },
    });
    if (result.status !== 0) {
      throw new Error(`${command} ${args.join(' ')} failed: ${result.stderr}`);
    }
  };
  run('go', [
    'build',
    '-ldflags',
    `-X github.com/aazam476/sentinel/internal/shared/buildinfo.stamped=${version}`,
    '-o',
    output,
    source,
  ]);
  const key = readFileSync(join(repository, 'testdata', 'release', 'development.key'), 'utf8');
  run(
    'go',
    [
      'run',
      './cmd/sentinel-release',
      'sign',
      '--name',
      'sentinel-agent',
      '--version',
      version,
      '--os',
      goos,
      '--arch',
      goarch,
      output,
    ],
    { SENTINEL_RELEASE_KEY: key },
  );
  return { build: output, signature: `${output}.sig` };
}
