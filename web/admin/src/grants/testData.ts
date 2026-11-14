import type { Schema } from '@local-server/shared';
import { CID } from '../consumers/testData';
import { settingsList, type Setting } from '../test/fixtures';

export const GID = '0192e5a0-0004-7000-8000-000000000004';
export const VID = '0192e5a0-0003-7000-8000-000000000003';
export const MID = '0192e5a0-000c-7000-8000-00000000000c';
export const RID = '0192e5a0-000d-7000-8000-00000000000d';

export function grantVersion(over: Partial<Schema<'GrantVersion'>> = {}, limits: Partial<Schema<'GrantLimits'>> = {}): Schema<'GrantVersion'> {
  return {
    id: VID,
    grant_id: GID,
    consumer_id: CID,
    version: 1,
    created_at: '2026-01-15T10:00:00Z',
    resource_request_id: null,
    limits: {
      models: [{ model: 'example-model', context_limit: 4096, max_output_tokens: null }],
      quota: { tokens: 100000 },
      quotas: [{ metric: 'tokens', limit: 100000, window_seconds: 86400 }],
      concurrency: 2,
      on_context_overflow: 'refuse',
      allowed_request_properties: null,
      expires_at: null,
      ...limits,
    },
    ...over,
  };
}

export function hostModel(over: Partial<Schema<'HostModel'>> = {}): Schema<'HostModel'> {
  return {
    id: MID,
    name: 'example-model',
    engine: 'ollama',
    allowed: true,
    preload: false,
    keep_alive_seconds: null,
    loaded: true,
    size_bytes: 4 * 1024 ** 3,
    ...over,
  };
}

export function policyRule(over: Partial<Schema<'PolicyRule'>> = {}): Schema<'PolicyRule'> {
  return {
    id: RID,
    grant_id: null,
    name: 'Background work at night',
    position: 0,
    enabled: true,
    version: 1,
    rule: { format_version: 1, when: { properties: { background: true } }, time_zone: 'Local', windows: [{ start: '00:00', end: '07:00' }], priority: 'background' },
    created_at: '2026-01-15T10:00:00Z',
    ...over,
  };
}

/** The fixture settings plus the host settings the grant, model and rule pages read. */
export function settingsWithDefaults(): Setting[] {
  const s = (key: string, value: unknown): Setting => ({ key, owner: 'host', value, default_value: value });
  return [
    ...settingsList(),
    s('scheduler.default_grant_concurrency', 1),
    s('gateway.default_context_tokens', 4096),
    s('gateway.max_tokens_ceiling', 8192),
    s('limits.quota_window', '24h'),
    s('limits.quota_window_min', '1m'),
    s('limits.quota_window_max', '720h'),
    s('scheduler.keep_alive', '5m'),
    s('admin.model_default_allowed', false),
    s('policy.rule.max_failures', 5),
  ];
}
