// SPDX-License-Identifier: Apache-2.0
import { vi } from 'vitest';

const bytes = (...values: number[]) => new Uint8Array(values).buffer;

/**
 * installFakeWebAuthn gives jsdom a navigator.credentials that answers like
 * a browser with a platform authenticator, or, with cancelled, like a person
 * closing the passkey prompt. It returns the create and get mocks.
 */
export function installFakeWebAuthn({ cancelled = false } = {}) {
  const common = {
    id: 'Bw',
    rawId: bytes(7),
    type: 'public-key',
    authenticatorAttachment: 'platform',
    getClientExtensionResults: () => ({}),
  };
  const answer = (response: object) =>
    cancelled
      ? Promise.reject(new DOMException('closed', 'NotAllowedError'))
      : Promise.resolve({ ...common, response });

  const create = vi.fn(() =>
    answer({
      clientDataJSON: bytes(1),
      attestationObject: bytes(2),
      getTransports: () => ['internal'],
    }),
  );
  const get = vi.fn(() =>
    answer({
      clientDataJSON: bytes(1),
      authenticatorData: bytes(3),
      signature: bytes(4),
      userHandle: null,
    }),
  );

  vi.stubGlobal('PublicKeyCredential', function PublicKeyCredential() {});
  Object.defineProperty(navigator, 'credentials', { value: { create, get }, configurable: true });
  return { create, get };
}

/** passkeyOptions is a minimal options object the helpers can decode. */
export const passkeyOptions = {
  options: {
    publicKey: {
      challenge: 'AQ',
      rp: { id: 'localhost' },
      user: { id: 'AQ', name: 'x', displayName: 'x' },
      rpId: 'localhost',
    },
  },
};
