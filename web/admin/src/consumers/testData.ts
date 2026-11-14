import type { Schema } from '@local-server/shared';

export const CID = '0192e5a0-0001-7000-8000-000000000001';
export const KID = '0192e5a0-0002-7000-8000-000000000002';

export function consumer(over: Partial<Schema<'Consumer'>> = {}): Schema<'Consumer'> {
  return {
    id: CID,
    kind: 'application',
    status: 'active',
    display_name: 'Example App',
    username: null,
    email: 'app@example.invalid',
    has_login: false,
    billing_mode: 'none',
    created_at: '2026-01-15T10:00:00Z',
    deleted_at: null,
    ...over,
  };
}

export function accessKey(over: Partial<Schema<'AccessKey'>> = {}): Schema<'AccessKey'> {
  return {
    id: KID,
    consumer_id: CID,
    prefix: 'abcd1234',
    name: 'Example key',
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
