// SPDX-License-Identifier: Apache-2.0
import {
  base64URLToBuffer,
  bufferToBase64URL,
  creationOptionsFromJSON,
  credentialToJSON,
  passkeysSupported,
  requestOptionsFromJSON,
} from './webauthn';

const bytes = (...values: number[]) => new Uint8Array(values).buffer;

/** defined fails the test when a value the test expects is missing. */
function defined<T>(value: T | null | undefined): T {
  if (value === null || value === undefined) {
    throw new Error('expected a value');
  }
  return value;
}

describe('base64url', () => {
  it.each([[[]], [[0]], [[255, 254]], [[1, 2, 3, 4, 5]], [[251, 255, 191]]])(
    'round-trips %j',
    (values) => {
      const buffer = bytes(...values);
      expect(new Uint8Array(base64URLToBuffer(bufferToBase64URL(buffer)))).toEqual(
        new Uint8Array(buffer),
      );
    },
  );

  it('uses the URL-safe alphabet without padding', () => {
    expect(bufferToBase64URL(bytes(251, 255, 191))).toBe('-_-_');
    expect(bufferToBase64URL(bytes(1))).toBe('AQ');
  });
});

describe('creationOptionsFromJSON', () => {
  it('decodes the challenge, user ID and excluded credentials', () => {
    const options = creationOptionsFromJSON({
      publicKey: {
        challenge: 'AQID',
        rp: { id: 'localhost', name: 'Sentinel' },
        user: { id: 'BAU', name: 'admin', displayName: 'admin' },
        excludeCredentials: [{ type: 'public-key', id: 'Bg' }],
      },
    });
    const publicKey = defined(options.publicKey);
    expect(new Uint8Array(publicKey.challenge as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]));
    expect(new Uint8Array(publicKey.user.id as ArrayBuffer)).toEqual(new Uint8Array([4, 5]));
    expect(
      new Uint8Array(defined(defined(publicKey.excludeCredentials)[0]).id as ArrayBuffer),
    ).toEqual(new Uint8Array([6]));
    expect(publicKey.user.name).toBe('admin');
  });

  it('refuses options without a publicKey', () => {
    expect(() => creationOptionsFromJSON({})).toThrow(/publicKey/);
  });
});

describe('requestOptionsFromJSON', () => {
  it('decodes the challenge and allowed credentials', () => {
    const options = requestOptionsFromJSON({
      publicKey: {
        challenge: 'AQ',
        rpId: 'localhost',
        allowCredentials: [{ type: 'public-key', id: 'Ag' }],
      },
    });
    expect(new Uint8Array(defined(options.publicKey).challenge as ArrayBuffer)).toEqual(
      new Uint8Array([1]),
    );
    expect(
      new Uint8Array(
        defined(defined(defined(options.publicKey).allowCredentials)[0]).id as ArrayBuffer,
      ),
    ).toEqual(new Uint8Array([2]));
  });

  it('leaves out allowed credentials when there are none', () => {
    expect(
      defined(requestOptionsFromJSON({ publicKey: { challenge: 'AQ' } }).publicKey)
        .allowCredentials,
    ).toBeUndefined();
  });
});

describe('credentialToJSON', () => {
  const base = {
    id: 'Bw',
    rawId: bytes(7),
    type: 'public-key',
    authenticatorAttachment: 'platform',
    getClientExtensionResults: () => ({}),
  };

  it('encodes a registration answer', () => {
    const credential = {
      ...base,
      response: {
        clientDataJSON: bytes(1),
        attestationObject: bytes(2),
        getTransports: () => ['internal'],
      },
    } as unknown as PublicKeyCredential;
    expect(credentialToJSON(credential)).toEqual({
      id: 'Bw',
      rawId: 'Bw',
      type: 'public-key',
      authenticatorAttachment: 'platform',
      clientExtensionResults: {},
      response: { clientDataJSON: 'AQ', attestationObject: 'Ag', transports: ['internal'] },
    });
  });

  it('encodes a sign-in answer, with and without a user handle', () => {
    const answer = (userHandle: ArrayBuffer | null) =>
      ({
        ...base,
        response: {
          clientDataJSON: bytes(1),
          authenticatorData: bytes(3),
          signature: bytes(4),
          userHandle,
        },
      }) as unknown as PublicKeyCredential;
    expect(credentialToJSON(answer(bytes(5))).response).toEqual({
      clientDataJSON: 'AQ',
      authenticatorData: 'Aw',
      signature: 'BA',
      userHandle: 'BQ',
    });
    expect(credentialToJSON(answer(null)).response).not.toHaveProperty('userHandle');
  });
});

describe('passkeysSupported', () => {
  it('is false where the browser has no WebAuthn', () => {
    expect(passkeysSupported()).toBe(false);
  });
});
