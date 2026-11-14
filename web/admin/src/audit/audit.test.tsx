import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import type { Schema } from '@local-server/shared';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';

const XSS = '<img src=x onerror=alert(1)>';

function event(id: string, over: Partial<Schema<'AuditEvent'>> = {}): Schema<'AuditEvent'> {
  return {
    id,
    occurred_at: '2026-01-15T10:05:00Z',
    actor: { principal_id: '0192e5a0-000b-7000-8000-00000000000b', kind: 'host_admin', display_name: 'Example Admin' },
    action: 'access_key.revoked',
    target_kind: 'access_key',
    target_id: '0192e5a0-0002-7000-8000-000000000002',
    details: {},
    ...over,
  };
}

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/audit-events', (c) =>
    c.query.get('cursor') === 'p2'
      ? { body: { data: [event('e3', { action: 'setting.changed', actor: null, target_kind: null, target_id: null })], next_cursor: null } }
      : {
          body: {
            data: [
              event('e1', {
                details: { key: 'admin.ui_poll_interval', fields: ['display_name', 'email'], name: XSS, nested: { prompt: 'secret prompt text' } },
              }),
              event('e2', { actor: { principal_id: '0192e5a0-0001-7000-8000-000000000001', kind: 'consumer', display_name: null } }),
            ],
            next_cursor: 'p2',
          },
        },
  );
  return f;
}

async function table() {
  return screen.findByRole('table', { name: 'Audited actions' });
}

describe('audit log page', () => {
  it('shows each event with its actor, action, target and plain details', async () => {
    renderApp(host(), { hash: '#/audit' });
    const rows = within(await table()).getAllByRole('row');
    expect(rows).toHaveLength(3);
    const first = rows[1]!.textContent!;
    expect(first).toContain('Example Admin');
    expect(first).toContain('Host admin');
    expect(first).toContain('access_key.revoked');
    expect(first).toContain('0192e5a0-0002-7000-8000-000000000002');
    expect(first).toContain('admin.ui_poll_interval');
    expect(first).toContain('display_name, email');
    expect(first).toContain(XSS);
    // An object detail is left out, so nothing nested in it is shown.
    expect(first).not.toContain('nested');
    expect(document.body.textContent).not.toContain('secret prompt text');
    expect(document.querySelector('img')).toBeNull();
    expect(rows[2]!.textContent).toContain('Consumer');
  });

  it('loads more through the cursor', async () => {
    const f = host();
    renderApp(f, { hash: '#/audit' });
    await table();
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => expect(within(screen.getByRole('table', { name: 'Audited actions' })).getAllByRole('row')).toHaveLength(4));
    const rows = within(screen.getByRole('table', { name: 'Audited actions' })).getAllByRole('row');
    expect(rows[3]!.textContent).toContain('local-server');
    expect(f.callsTo('GET', '/admin/v1/audit-events')[1]!.query.get('cursor')).toBe('p2');
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('filters by action and days', async () => {
    const f = host();
    renderApp(f, { hash: '#/audit' });
    await table();
    fireEvent.change(screen.getByLabelText('Action'), { target: { value: ' setting.changed ' } });
    fireEvent.change(screen.getByLabelText('From day'), { target: { value: '2026-01-10' } });
    fireEvent.change(screen.getByLabelText('To day (included)'), { target: { value: '2026-01-15' } });
    fireEvent.click(screen.getByRole('button', { name: 'Show' }));
    await waitFor(() => expect(f.callsTo('GET', '/admin/v1/audit-events')).toHaveLength(2));
    const q = f.callsTo('GET', '/admin/v1/audit-events')[1]!.query;
    expect(q.get('action')).toBe('setting.changed');
    expect(q.get('from')).toBe(new Date(2026, 0, 10).toISOString());
    expect(q.get('to')).toBe(new Date(2026, 0, 16).toISOString());
    expect(q.get('cursor')).toBeNull();
  });

  it('shows the API error', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/audit-events', errorReply(500, 'internal_error', 'Something went wrong.'));
    renderApp(f, { hash: '#/audit' });
    expect(await screen.findByText('Something went wrong.')).toBeTruthy();
    expect(screen.getByText('The audit log could not be read')).toBeTruthy();
  });
});
