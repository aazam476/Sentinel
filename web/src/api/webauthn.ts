// SPDX-License-Identifier: Apache-2.0

/**
 * The server sends passkey options as JSON, with binary values as base64url
 * strings, and expects the browser's answer in the same form. The browser's
 * navigator.credentials API works with ArrayBuffers. These helpers convert
 * between the two. (Newer browsers have PublicKeyCredential.parse*FromJSON
 * and toJSON for this, but not all the browsers Sentinel supports yet.)
 */

type JSONObject = Record<string, unknown>;

export function base64URLToBuffer(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

export function bufferToBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function publicKeyOf(options: JSONObject): JSONObject {
  const publicKey = options.publicKey;
  if (typeof publicKey !== 'object' || publicKey === null) {
    throw new Error('The server sent passkey options without a publicKey.');
  }
  return publicKey as JSONObject;
}

function decodeDescriptors(descriptors: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(descriptors)) {
    return undefined;
  }
  return descriptors.map((descriptor: JSONObject) => ({
    ...(descriptor as unknown as PublicKeyCredentialDescriptor),
    id: base64URLToBuffer(descriptor.id as string),
  }));
}

/** creationOptionsFromJSON prepares options for navigator.credentials.create(). */
export function creationOptionsFromJSON(options: JSONObject): CredentialCreationOptions {
  const publicKey = publicKeyOf(options);
  const user = publicKey.user as JSONObject;
  return {
    publicKey: {
      ...(publicKey as unknown as PublicKeyCredentialCreationOptions),
      challenge: base64URLToBuffer(publicKey.challenge as string),
      user: {
        ...(user as unknown as PublicKeyCredentialUserEntity),
        id: base64URLToBuffer(user.id as string),
      },
      excludeCredentials: decodeDescriptors(publicKey.excludeCredentials),
    },
  };
}

/** requestOptionsFromJSON prepares options for navigator.credentials.get(). */
export function requestOptionsFromJSON(options: JSONObject): CredentialRequestOptions {
  const publicKey = publicKeyOf(options);
  return {
    publicKey: {
      ...(publicKey as unknown as PublicKeyCredentialRequestOptions),
      challenge: base64URLToBuffer(publicKey.challenge as string),
      allowCredentials: decodeDescriptors(publicKey.allowCredentials),
    },
  };
}

/** credentialToJSON serializes the browser's answer for the server. */
export function credentialToJSON(credential: PublicKeyCredential): JSONObject {
  const response = credential.response;
  const encoded: JSONObject = { clientDataJSON: bufferToBase64URL(response.clientDataJSON) };

  if ('attestationObject' in response) {
    const attestation = response as AuthenticatorAttestationResponse;
    encoded.attestationObject = bufferToBase64URL(attestation.attestationObject);
    encoded.transports = attestation.getTransports();
  } else {
    const assertion = response as AuthenticatorAssertionResponse;
    encoded.authenticatorData = bufferToBase64URL(assertion.authenticatorData);
    encoded.signature = bufferToBase64URL(assertion.signature);
    if (assertion.userHandle !== null) {
      encoded.userHandle = bufferToBase64URL(assertion.userHandle);
    }
  }

  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: encoded,
  };
}

/** passkeysSupported reports whether this browser can use passkeys at all. */
export function passkeysSupported(): boolean {
  return typeof window.PublicKeyCredential === 'function' && 'credentials' in navigator;
}
