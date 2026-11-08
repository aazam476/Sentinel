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
 * installCommands builds the install commands for a new token. The TLS
 * fingerprint is included only when the server uses its own self-signed
 * certificate, which the agent then pins.
 */
export function installCommands(
  created: CreateEnrollmentTokenResponse,
  server: string,
): InstallCommand[] {
  const flags = [
    `--server ${quote(server)}`,
    `--token ${quote(created.secret)}`,
    `--fingerprint ${quote(created.serverFingerprint)}`,
  ];
  if (created.tlsFingerprint !== '') {
    flags.push(`--tls-fingerprint ${quote(created.tlsFingerprint)}`);
  }
  return [
    {
      system: 'Linux and macOS',
      where: 'In a terminal, in the folder with the sentinel-agent program:',
      command: `sudo ./sentinel-agent install ${flags.join(' ')}`,
    },
    {
      system: 'Windows',
      where: 'In PowerShell run as Administrator, in the folder with sentinel-agent.exe:',
      command: `.\\sentinel-agent.exe install ${flags.join(' ')}`,
    },
  ];
}

/**
 * quote wraps a value in single quotes when it has anything a shell would
 * treat specially. Tokens and fingerprints are base64url, so this only
 * matters for unusual server URLs.
 */
function quote(value: string): string {
  return /^[A-Za-z0-9_\-.:/=]+$/.test(value) ? value : `'${value.replaceAll("'", "'\\''")}'`;
}
