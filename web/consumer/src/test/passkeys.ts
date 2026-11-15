import { vi } from 'vitest';

// jsdom has no WebAuthn. These install a fake navigator.credentials and PublicKeyCredential, so a
// test decides what the "authenticator" answers, and remove them again after each test.

export function bytes(s: string): ArrayBuffer {
  return new TextEncoder().encode(s).buffer as ArrayBuffer;
}

export function text(b: unknown): string {
  return new TextDecoder().decode(new Uint8Array(b as ArrayBuffer));
}

/** Installs a fake WebAuthn; returns the create and get mocks. */
export function installPasskeys() {
  const create = vi.fn<(o: CredentialCreationOptions) => Promise<Credential | null>>();
  const get = vi.fn<(o: CredentialRequestOptions) => Promise<Credential | null>>();
  Object.defineProperty(window, 'PublicKeyCredential', { value: function PublicKeyCredential() {}, configurable: true, writable: true });
  Object.defineProperty(navigator, 'credentials', { value: { create, get }, configurable: true, writable: true });
  return { create, get };
}

export function removePasskeys() {
  Reflect.deleteProperty(window, 'PublicKeyCredential');
  Object.defineProperty(navigator, 'credentials', { value: undefined, configurable: true, writable: true });
}

/** A new passkey as a browser's create() answers it. */
export function fakeAttestation(): Credential {
  return {
    id: 'Y3JlZA',
    rawId: bytes('cred'),
    type: 'public-key',
    authenticatorAttachment: 'platform',
    response: { clientDataJSON: bytes('client-data'), attestationObject: bytes('attestation'), getTransports: () => ['internal'] },
    getClientExtensionResults: () => ({}),
  } as unknown as Credential;
}

/** A passkey login as a browser's get() answers it. */
export function fakeAssertion(): Credential {
  return {
    id: 'Y3JlZA',
    rawId: bytes('cred'),
    type: 'public-key',
    authenticatorAttachment: null,
    response: {
      clientDataJSON: bytes('client-data'),
      authenticatorData: bytes('auth-data'),
      signature: bytes('signature'),
      userHandle: bytes('handle'),
    },
    getClientExtensionResults: () => ({}),
  } as unknown as Credential;
}

/** The DOMException a browser throws when the person cancels or the prompt times out. */
export function cancelled(): DOMException {
  return new DOMException('The operation either timed out or was not allowed.', 'NotAllowedError');
}
