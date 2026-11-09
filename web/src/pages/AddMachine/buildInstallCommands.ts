// SPDX-License-Identifier: Apache-2.0
import type { CreateEnrollmentTokenResponse } from '../../api/types.gen';

/** InstallCommand is the command to run on one kind of machine. */
export interface InstallCommand {
  system: string;
  where: string;
  command: string;
}

/**
 * serverURL is the address agents connect to: this page's origin and the
 * server's base path, from the page's <base> element.
 */
export function serverURL(baseURI: string): string {
  return new URL('.', baseURI).toString().replace(/\/$/, '');
}

/**
 * installCommands builds the install commands for a new token: scripts the
 * server serves, which download the agent and check its release signature
 * (SEC-34), and the same install for a machine that already has the
 * program. The TLS fingerprint is included only when the server uses its
 * own self-signed certificate, which the agent (and curl) then pins; the
 * PowerShell script can't pin one, so it is offered only without it.
 */
export function installCommands(
  created: CreateEnrollmentTokenResponse,
  server: string,
): InstallCommand[] {
  const selfSigned = created.tlsFingerprint !== '';
  const flags = [
    `--server ${quote(server)}`,
    `--token ${quote(created.secret)}`,
    `--fingerprint ${quote(created.serverFingerprint)}`,
  ];
  if (selfSigned) {
    flags.push(`--tls-fingerprint ${quote(created.tlsFingerprint)}`);
  }
  const pin = selfSigned
    ? `--insecure --pinnedpubkey ${quote(curlPin(created.tlsFingerprint))} `
    : '';
  const commands: InstallCommand[] = [
    {
      system: 'Linux and macOS',
      where: 'In a terminal:',
      command: `curl -fsSL ${pin}${quote(`${server}/install/agent.sh`)} | sudo sh -s -- ${flags.join(' ')}`,
    },
  ];
  if (!selfSigned) {
    commands.push({
      system: 'Windows',
      where: 'In PowerShell run as Administrator:',
      command:
        `& ([scriptblock]::Create((iwr -UseBasicParsing '${server.replaceAll("'", "''")}/install/agent.ps1').Content)) ` +
        `-Server ${psQuote(server)} -Token ${psQuote(created.secret)} -Fingerprint ${psQuote(created.serverFingerprint)}`,
    });
  }
  commands.push(
    {
      system: 'Linux and macOS, with the program already copied',
      where: 'In a terminal, in the folder with the sentinel-agent program:',
      command: `sudo ./sentinel-agent install ${flags.join(' ')}`,
    },
    {
      system: 'Windows, with the program already copied',
      where: 'In PowerShell run as Administrator, in the folder with sentinel-agent.exe:',
      command: `.\\sentinel-agent.exe install ${flags.join(' ')}`,
    },
  );
  return commands;
}

/**
 * curlPin turns a certificate key fingerprint ("SHA256:" and base64url)
 * into curl's --pinnedpubkey form ("sha256//" and padded base64).
 */
export function curlPin(fingerprint: string): string {
  let digest = fingerprint
    .replace(/^SHA256:/, '')
    .replaceAll('-', '+')
    .replaceAll('_', '/');
  while (digest.length % 4 !== 0) {
    digest += '=';
  }
  return `sha256//${digest}`;
}

/** psQuote quotes a value for PowerShell. */
function psQuote(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

/**
 * quote wraps a value in single quotes when it has anything a shell would
 * treat specially. Tokens and fingerprints are base64url, so this only
 * matters for unusual server URLs.
 */
function quote(value: string): string {
  return /^[A-Za-z0-9_\-.:/=+]+$/.test(value) ? value : `'${value.replaceAll("'", "'\\''")}'`;
}
