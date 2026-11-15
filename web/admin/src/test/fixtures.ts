import { createFakeFetch, errorReply, type FakeFetch } from '@local-server/shared/test';
import type { Schema } from '@local-server/shared';

export type Setting = Schema<'Setting'>;

export const BASE = 'http://admin.test';

export const user: Schema<'AdminUser'> = { id: '0192e5a0-000b-7000-8000-00000000000b', username: 'example-admin', created_at: '2026-01-15T10:00:00Z' };

export function hostInfo(over: Partial<Schema<'HostInfo'>> = {}): Schema<'HostInfo'> {
  return {
    host_kid: 'ZXhhbXBsZS1pZGVudGl0eS1wdWJsaWMta2V5',
    sharing_state: 'sharing',
    paused_at: null,
    sharing_resumes_at: null,
    software_version: '0.0.0-example',
    engine_state: 'available',
    engine_version: '0.0.0-engine',
    tailnet: { state: 'stopped', message: 'Tailscale is switched off.', fix: 'Switch Tailscale on.', dns_name: null, listeners: [] },
    ...over,
  };
}

const GiB = 1024 ** 3;

export function sample(at = '2026-01-15T10:00:00Z'): Schema<'HostMetricsSample'> {
  return {
    sampled_at: at,
    total: { memory_bytes: { value: 16 * GiB, method: 'os-total-counter' }, cpu_percent: { value: 12.5, method: 'os-cpu-counter' } },
    share_budget_slice: { memory_bytes: { value: 8 * GiB, method: 'process-rss' }, cpu_percent: { value: 6.25, method: 'process-cpu' } },
    other_apps: { memory_bytes: { value: 4 * GiB, method: 'total-minus-slice' }, cpu_percent: { value: 3.5, method: 'total-minus-slice' } },
    unattributed: { memory_bytes: { value: null, method: 'access-denied' }, cpu_percent: { value: null, method: 'access-denied' } },
  };
}

export function settingsList(over: Record<string, Partial<Setting>> = {}, remove: string[] = []): Setting[] {
  const base: Setting[] = [
    { key: 'admin.ui_poll_interval', owner: 'host', value: '30s', default_value: '30s', description: 'poll' },
    { key: 'scheduler.memory_budget_bytes', owner: 'host', value: 24 * GiB, default_value: 0, description: 'budget' },
    { key: 'host.quiet_hours.enabled', owner: 'host', value: false, default_value: false },
    { key: 'host.quiet_hours.time_zone', owner: 'host', value: 'Local', default_value: 'Local' },
    { key: 'host.quiet_hours.start', owner: 'host', value: '22:00', default_value: '22:00' },
    { key: 'host.quiet_hours.end', owner: 'host', value: '07:00', default_value: '07:00' },
  ];
  const out = base.map((s) => ({ ...s, ...over[s.key] }));
  return out.filter((s) => !remove.includes(s.key));
}

/** A fake admin API of a host with an admin and a valid session; tests override routes. */
export function fakeHost(): FakeFetch {
  const f = createFakeFetch();
  f.on('GET', '/admin/v1/setup', { body: { admin_exists: true } });
  f.on('GET', '/admin/v1/session', { body: user });
  f.on('GET', '/admin/v1/settings', { body: { data: settingsList() } });
  f.on('GET', '/admin/v1/host', { body: hostInfo() });
  f.on('GET', '/admin/v1/host-metrics', { body: sample() });
  f.on('GET', '/admin/v1/host-metrics/history', { body: { data: [sample('2026-01-15T09:59:00Z'), sample()] } });
  f.on('GET', '/admin/v1/resource-requests', errorReply(501, 'not_implemented', 'This operation is not implemented yet.'));
  return f;
}

export function resourceRequest(id: string, over: Partial<Schema<'ResourceRequest'>> = {}): Schema<'ResourceRequest'> {
  return {
    id,
    consumer_id: '0192e5a0-0001-7000-8000-000000000001',
    status: 'open',
    message: 'Please <b>more</b>',
    ask: { models: ['example-model'] },
    created_at: '2026-01-15T10:00:00Z',
    expires_at: '2026-01-16T10:00:00Z',
    decided_at: null,
    resulting_grant_version_id: null,
    ...over,
  } as Schema<'ResourceRequest'>;
}
