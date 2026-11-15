/**
 * The browser side of a passkey ceremony (req_001 D-13). The host's begin routes answer the
 * WebAuthn options as JSON, with every binary field (challenge, user id, credential ids) in
 * base64url without padding; the browser's navigator.credentials wants them as ArrayBuffers and
 * answers ArrayBuffers, which go back to the finish routes as base64url in the shape of the
 * WebAuthn Level 3 `toJSON()`. The timeout and every other option come from the host as they are
 * (req_001 D-38); nothing is set here.
 */

type Json = Record<string, unknown>;

/** A passkey step that failed in the browser, with a message to show as it is. */
export class PasskeyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'PasskeyError';
  }
}

/** Whether this browser can create and use passkeys at all. */
export function passkeysSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.PublicKeyCredential === 'function' &&
    typeof navigator !== 'undefined' &&
    !!navigator.credentials &&
    typeof navigator.credentials.create === 'function' &&
    typeof navigator.credentials.get === 'function'
  );
}

/** Decodes base64url, with or without padding, into bytes. */
export function fromBase64url(s: string): ArrayBuffer {
  const t = s.replace(/=+$/, '').replace(/-/g, '+').replace(/_/g, '/');
  const bin = atob(t + '='.repeat((4 - (t.length % 4)) % 4));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

/** Encodes bytes as base64url without padding. */
export function toBase64url(b: ArrayBuffer | ArrayBufferView): string {
  // isView, not instanceof: a buffer can come from another realm (a frame, a test environment).
  const bytes = ArrayBuffer.isView(b) ? new Uint8Array(b.buffer, b.byteOffset, b.byteLength) : new Uint8Array(b);
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function unreadable(): PasskeyError {
  return new PasskeyError('The host sent passkey options this page cannot read. Reload the page and try again.');
}

function obj(v: unknown): Json {
  if (!v || typeof v !== 'object' || Array.isArray(v)) throw unreadable();
  return v as Json;
}

function bytes(v: unknown): ArrayBuffer {
  if (typeof v !== 'string') throw unreadable();
  try {
    return fromBase64url(v);
  } catch {
    throw unreadable();
  }
}

function descriptors(v: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (v === undefined || v === null) return undefined;
  if (!Array.isArray(v)) throw unreadable();
  return v.map((d) => {
    const o = obj(d);
    return { ...o, id: bytes(o.id) } as PublicKeyCredentialDescriptor;
  });
}

/** Some encoders wrap the options in `publicKey`, as navigator.credentials takes them; accept both. */
function unwrap(options: Json): Json {
  return options.publicKey && typeof options.publicKey === 'object' ? obj(options.publicKey) : options;
}

/** The creation options of a begin route, as navigator.credentials.create() takes them. */
export function creationOptions(options: Json): PublicKeyCredentialCreationOptions {
  const o = unwrap(options);
  const user = obj(o.user);
  const out: Json = { ...o, challenge: bytes(o.challenge), user: { ...user, id: bytes(user.id) } };
  const exclude = descriptors(o.excludeCredentials);
  if (exclude) out.excludeCredentials = exclude;
  else delete out.excludeCredentials;
  return out as unknown as PublicKeyCredentialCreationOptions;
}

/** The request options of a begin route, as navigator.credentials.get() takes them. */
export function requestOptions(options: Json): PublicKeyCredentialRequestOptions {
  const o = unwrap(options);
  const out: Json = { ...o, challenge: bytes(o.challenge) };
  const allow = descriptors(o.allowCredentials);
  if (allow) out.allowCredentials = allow;
  else delete out.allowCredentials;
  return out as unknown as PublicKeyCredentialRequestOptions;
}

function common(c: PublicKeyCredential): Json {
  const out: Json = {
    id: c.id,
    rawId: toBase64url(c.rawId),
    type: c.type,
    clientExtensionResults: typeof c.getClientExtensionResults === 'function' ? c.getClientExtensionResults() : {},
  };
  if (c.authenticatorAttachment) out.authenticatorAttachment = c.authenticatorAttachment;
  return out;
}

/** A new passkey from navigator.credentials.create() as the JSON the finish routes take. */
export function attestationJSON(c: PublicKeyCredential): Json {
  const r = c.response as AuthenticatorAttestationResponse;
  const response: Json = {
    clientDataJSON: toBase64url(r.clientDataJSON),
    attestationObject: toBase64url(r.attestationObject),
  };
  if (typeof r.getTransports === 'function') response.transports = r.getTransports();
  return { ...common(c), response };
}

/** A passkey login from navigator.credentials.get() as the JSON the finish route takes. */
export function assertionJSON(c: PublicKeyCredential): Json {
  const r = c.response as AuthenticatorAssertionResponse;
  const response: Json = {
    clientDataJSON: toBase64url(r.clientDataJSON),
    authenticatorData: toBase64url(r.authenticatorData),
    signature: toBase64url(r.signature),
  };
  if (r.userHandle) response.userHandle = toBase64url(r.userHandle);
  return { ...common(c), response };
}

/** A browser refusal in plain language. The host is not told why the browser refused. */
function browserError(e: unknown, creating: boolean): PasskeyError {
  const name = e && typeof e === 'object' && 'name' in e ? String((e as { name: unknown }).name) : '';
  switch (name) {
    case 'NotAllowedError':
      return new PasskeyError('The passkey step was cancelled or took too long. Try again.');
    case 'InvalidStateError':
      return new PasskeyError(
        creating ? 'This device already holds a passkey for this account.' : 'This device cannot use that passkey here.',
      );
    case 'SecurityError':
      return new PasskeyError("Passkeys need this page to be opened by the host's name over HTTPS, not by an IP address.");
    case 'NotSupportedError':
      return new PasskeyError('This device or browser cannot make the kind of passkey the host asks for.');
    default:
      return new PasskeyError('The browser could not complete the passkey step. Try again or use a password.');
  }
}

function notSupported(): PasskeyError {
  return new PasskeyError('This browser cannot use passkeys. Use a password instead.');
}

function asCredential(c: Credential | null): PublicKeyCredential {
  if (!c || (c as PublicKeyCredential).type !== 'public-key' || !(c as PublicKeyCredential).response) {
    throw new PasskeyError('The browser did not return a passkey. Try again.');
  }
  return c as PublicKeyCredential;
}

/** Creates a passkey with the options of a begin route and returns it as JSON for the finish route. */
export async function createPasskey(options: Json): Promise<Json> {
  if (!passkeysSupported()) throw notSupported();
  const publicKey = creationOptions(options);
  let c: Credential | null;
  try {
    c = await navigator.credentials.create({ publicKey });
  } catch (e) {
    throw browserError(e, true);
  }
  return attestationJSON(asCredential(c));
}

/** Uses a passkey with the options of a begin route and returns the answer as JSON for the finish route. */
export async function getPasskey(options: Json): Promise<Json> {
  if (!passkeysSupported()) throw notSupported();
  const publicKey = requestOptions(options);
  let c: Credential | null;
  try {
    c = await navigator.credentials.get({ publicKey });
  } catch (e) {
    throw browserError(e, false);
  }
  return assertionJSON(asCredential(c));
}
