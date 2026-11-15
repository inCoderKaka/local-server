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

export const CID = consumer.id;

/** A grant version of the example consumer: one model, a daily token quota, two requests at once. */
export function grantVersion(over: Partial<Schema<'GrantLimits'>> = {}, version: Partial<Schema<'GrantVersion'>> = {}): Schema<'GrantVersion'> {
  return {
    id: '0192e5a0-0003-7000-8000-000000000003',
    grant_id: '0192e5a0-0004-7000-8000-000000000004',
    consumer_id: CID,
    version: 1,
    created_at: '2026-01-15T10:00:00Z',
    resource_request_id: null,
    limits: {
      models: [{ model: 'example-model', context_limit: 4096, max_output_tokens: 1024 }],
      quotas: [{ metric: 'tokens', limit: 100000, window_seconds: 86400 }],
      concurrency: 2,
      on_context_overflow: 'refuse',
      allowed_request_properties: null,
      expires_at: null,
      ...over,
    },
    ...version,
  };
}

export function rollup(over: Partial<Schema<'UsageRollup'>> = {}): Schema<'UsageRollup'> {
  return {
    bucket_start: '2026-01-15T10:00:00Z',
    granularity: 'hour',
    consumer_id: CID,
    access_key_id: '0192e5a0-0002-7000-8000-000000000002',
    model: 'example-model',
    requests: 2,
    prompt_tokens: 100,
    completion_tokens: 50,
    total_tokens: 150,
    ...over,
  };
}

export function resourceRequest(over: Partial<Schema<'ResourceRequest'>> = {}): Schema<'ResourceRequest'> {
  return {
    id: '0192e5a0-0006-7000-8000-000000000006',
    consumer_id: CID,
    status: 'open',
    message: 'Could I have a longer context?',
    ask: { models: ['example-model'], context_limit: 8192 },
    created_at: '2026-01-15T10:00:00Z',
    expires_at: '2026-01-29T10:00:00Z',
    decided_at: null,
    resulting_grant_version_id: null,
    ...over,
  };
}

export function accessKey(over: Partial<Schema<'AccessKey'>> = {}): Schema<'AccessKey'> {
  return {
    id: '0192e5a0-0002-7000-8000-000000000002',
    consumer_id: CID,
    prefix: 'abcd1234',
    name: 'Laptop key',
    status: 'active',
    narrowing: null,
    bound_node: null,
    bound_node_name: null,
    created_at: '2026-01-15T10:00:00Z',
    expires_at: null,
    revoked_at: null,
    ...over,
  };
}

export const settings: Schema<'Setting'>[] = [
  { key: 'jobs.deadline', owner: 'consumer', value: '24h', default_value: '24h', description: 'How long a job may wait queued.' },
  { key: 'jobs.result_ttl', owner: 'consumer', value: '1h', default_value: '24h', description: "How long a job's result is kept." },
];

/**
 * A host where the example consumer is logged in, with a grant, no usage, no requests, no keys
 * and two settings of its own. Tests override routes (the last registration wins).
 */
export function signedIn(): FakeFetch {
  const f = createFakeFetch();
  f.on('GET', '/v1/me', { body: consumer });
  f.on('GET', '/v1/me/limits', { body: grantVersion() });
  f.on('GET', '/v1/usage', { body: { data: [], next_cursor: null } });
  f.on('GET', '/v1/resource-requests', { body: { data: [], next_cursor: null } });
  f.on('GET', '/v1/me/access-keys', { body: { data: [] } });
  f.on('GET', '/v1/me/settings', { body: { data: settings } });
  return f;
}

/** The host's answer when it has not given the consumer any limits yet (req_001 D-150). */
export const noGrant = errorReply(404, 'not_found', 'The host has not given this account any limits yet.');

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
