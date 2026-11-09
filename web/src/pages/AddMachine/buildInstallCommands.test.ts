// SPDX-License-Identifier: Apache-2.0
import type { CreateEnrollmentTokenResponse } from '../../api/types.gen';
import { curlPin, installCommands, serverURL } from './buildInstallCommands';

const created: CreateEnrollmentTokenResponse = {
  token: {
    id: 1,
    note: '',
    createdBy: 'admin',
    createdAt: 0,
    expiresAt: 1,
    maxUses: 1,
    useCount: 0,
    revoked: false,
    usable: true,
  },
  secret: 'abc_DEF-123',
  serverFingerprint: 'SHA256:server',
  tlsFingerprint: '',
};

describe('serverURL', () => {
  it.each([
    ['https://sentinel.example.com/', 'https://sentinel.example.com'],
    ['https://example.com/sentinel/', 'https://example.com/sentinel'],
  ])('turns base %j into %j', (base, expected) => {
    expect(serverURL(base)).toBe(expected);
  });
});

describe('installCommands', () => {
  it('builds script commands that check the agent, and commands for a copied program', () => {
    const commands = installCommands(created, 'https://sentinel.example.com');
    expect(commands.map((command) => command.system)).toEqual([
      'Linux and macOS',
      'Windows',
      'Linux and macOS, with the program already copied',
      'Windows, with the program already copied',
    ]);
    expect(commands[0]?.command).toBe(
      'curl -fsSL https://sentinel.example.com/install/agent.sh | sudo sh -s -- --server https://sentinel.example.com --token abc_DEF-123 --fingerprint SHA256:server',
    );
    expect(commands[1]?.command).toBe(
      "& ([scriptblock]::Create((iwr -UseBasicParsing 'https://sentinel.example.com/install/agent.ps1').Content)) -Server 'https://sentinel.example.com' -Token 'abc_DEF-123' -Fingerprint 'SHA256:server'",
    );
    expect(commands[2]?.command).toBe(
      'sudo ./sentinel-agent install --server https://sentinel.example.com --token abc_DEF-123 --fingerprint SHA256:server',
    );
    expect(commands[3]?.command).toMatch(/^\.\\sentinel-agent\.exe install --server /);
  });

  it('pins a self-signed server for curl and the agent, and leaves out the PowerShell script', () => {
    const commands = installCommands({ ...created, tlsFingerprint: 'SHA256:ab-c_d' }, 'https://h');
    expect(commands[0]?.command).toBe(
      'curl -fsSL --insecure --pinnedpubkey sha256//ab+c/d== https://h/install/agent.sh | sudo sh -s -- --server https://h --token abc_DEF-123 --fingerprint SHA256:server --tls-fingerprint SHA256:ab-c_d',
    );
    expect(commands.map((command) => command.system)).not.toContain('Windows');
  });

  it('quotes a server URL with characters a shell or PowerShell would interpret', () => {
    const [unix, windows] = installCommands(created, "https://h/it's here");
    expect(unix?.command).toContain(`--server 'https://h/it'\\''s here'`);
    expect(windows?.command).toContain(`-Server 'https://h/it''s here'`);
  });

  it('turns a fingerprint into curl’s pin', () => {
    expect(curlPin('SHA256:AAAA')).toBe('sha256//AAAA');
  });
});
