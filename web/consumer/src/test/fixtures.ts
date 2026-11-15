import { createFakeFetch, errorReply, type FakeFetch } from '@local-server/shared/test';
import type { Schema } from '@local-server/shared';

export const BASE = 'http://consumer.test';

export const consumer: Schema<'Consumer'> = {
  id: '0192e5a0-0001-7000-8000-000000000001',
  kind: 'individual',
  status: 'active',
  display_name: 'Example Consumer',
  username: 'example-user',
  email: null,
  has_login: true,
  billing_mode: 'none',
  created_at: '2026-01-15T10:00:00Z',
  deleted_at: null,
};

/** A host where nobody is logged in: GET /v1/me is a 401. */
export function signedOut(): FakeFetch {
  const f = createFakeFetch();
  f.on('GET', '/v1/me', errorReply(401, null, 'Log in first: there is no valid session.'));
  return f;
}

/** A host where the example consumer is logged in. */
export function signedIn(): FakeFetch {
  const f = createFakeFetch();
  f.on('GET', '/v1/me', { body: consumer });
  return f;
}

/** Creation options as the host's begin route answers them (go-webauthn, base64url ids). */
export const creationOptions = {
  rp: { id: 'host.example.invalid', name: 'local-server' },
  user: { id: 'dXNlci1oYW5kbGU', name: 'Example Consumer', displayName: 'Example Consumer' },
  challenge: 'Y2hhbGxlbmdl',
  pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
  timeout: 300000,
  authenticatorSelection: { residentKey: 'required', requireResidentKey: true, userVerification: 'required' },
};

/** Request options of a discoverable login as the host's begin route answers them. */
export const requestOptions = {
  challenge: 'bG9naW4tY2hhbGxlbmdl',
  rpId: 'host.example.invalid',
  timeout: 300000,
  userVerification: 'required',
};
