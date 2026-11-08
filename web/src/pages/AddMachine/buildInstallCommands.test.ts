// SPDX-License-Identifier: Apache-2.0
import type { CreateEnrollmentTokenResponse } from '../../api/types.gen';
import { installCommands, serverURL } from './buildInstallCommands';

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
  it('builds a sudo command and a PowerShell command with the token and fingerprint', () => {
    const [unix, windows] = installCommands(created, 'https://sentinel.example.com');
    expect(unix?.command).toBe(
      'sudo ./sentinel-agent install --server https://sentinel.example.com --token abc_DEF-123 --fingerprint SHA256:server',
    );
    expect(windows?.command).toMatch(/^\.\\sentinel-agent\.exe install --server /);
  });

  it('adds the TLS fingerprint for a self-signed server', () => {
    const [unix] = installCommands({ ...created, tlsFingerprint: 'SHA256:tls' }, 'https://h');
    expect(unix?.command).toMatch(/--tls-fingerprint SHA256:tls$/);
  });

  it('quotes a server URL with characters a shell would interpret', () => {
    const [unix] = installCommands(created, "https://h/it's here");
    expect(unix?.command).toContain(`--server 'https://h/it'\\''s here'`);
  });
});
