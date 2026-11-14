import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { Schema } from '@local-server/shared';
import { errorReply } from '@local-server/shared/test';
import { fakeHost, settingsList } from '../test/fixtures';
import { renderApp } from '../test/render';

const IID = '0192e5a0-000a-7000-8000-00000000000a';
const TOKEN = 'ls_invite_abcd_' + 'T'.repeat(24);

function invite(over: Partial<Schema<'Invite'>> = {}): Schema<'Invite'> {
  return {
    id: IID,
    status: 'pending',
    note: 'For <script>alert(1)</script> Sam',
    created_at: '2026-01-15T10:00:00Z',
    expires_at: '2026-01-16T10:00:00Z',
    redeemed_consumer_id: null,
    ...over,
  };
}

function host() {
  const f = fakeHost();
  const settings = [...settingsList(), { key: 'invites.ttl', owner: 'host' as const, value: '36h', default_value: '24h' }];
  f.on('GET', '/admin/v1/settings', { body: { data: settings } });
  f.on('GET', '/admin/v1/invites', {
    body: {
      data: [invite(), invite({ id: 'i2', status: 'redeemed', note: null, redeemed_consumer_id: '0192e5a0-0001-7000-8000-000000000001' })],
      next_cursor: null,
    },
  });
  return f;
}

function tokenAnywhere(): boolean {
  const inputs = [...document.querySelectorAll('input')].map((i) => i.value);
  return document.body.textContent!.includes(TOKEN) || inputs.some((v) => v.includes(TOKEN)) || window.location.href.includes(TOKEN);
}

describe('invites', () => {
  it('lists invites with their notes as text', async () => {
    renderApp(host(), { hash: '#/invites' });
    const table = await screen.findByRole('table', { name: 'Invites, newest first' });
    const rows = within(table).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('For <script>alert(1)</script> Sam');
    expect(document.querySelector('main script')).toBeNull();
    expect(rows[1]!.textContent).toContain('Pending');
    expect(rows[2]!.textContent).toContain('Redeemed');
    expect(within(rows[2]!).getByRole('link', { name: 'Open consumer' }).getAttribute('href')).toBe('#/consumers/0192e5a0-0001-7000-8000-000000000001');
    expect(within(rows[2]!).queryByRole('button', { name: /Revoke/ })).toBeNull();
  });

  it('creates an invite with the host default expiry and shows its link once', async () => {
    const f = host();
    f.on('POST', '/admin/v1/invites', {
      status: 201,
      body: { invite: invite({ id: 'i3', note: 'Sam' }), token: TOKEN, link: `https://host.example.invalid/invite#${TOKEN}` },
    });
    renderApp(f, { hash: '#/invites' });
    const form = await screen.findByRole('form', { name: 'New invite' });
    // The default is the server's setting, shown as it is set.
    expect(await within(form).findByLabelText(/The host's invite validity \(setting invites.ttl: 36h\)/)).toBeTruthy();
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Note (optional)'), 'Sam');
    await u.click(within(form).getByRole('button', { name: 'Create invite' }));

    const panel = await screen.findByRole('region', { name: 'Invite link' });
    expect((within(panel).getByLabelText('Invite link') as HTMLInputElement).value).toBe(`https://host.example.invalid/invite#${TOKEN}`);
    expect(panel.textContent).not.toContain('relative');
    expect(f.callsTo('POST', '/admin/v1/invites')[0]!.body).toEqual({ note: 'Sam' });
    await waitFor(() => expect(f.callsTo('GET', '/admin/v1/invites').length).toBeGreaterThanOrEqual(2));
    expect(window.location.href).not.toContain(TOKEN);

    await u.click(within(panel).getByRole('button', { name: 'I have copied it' }));
    expect(screen.queryByRole('region', { name: 'Invite link' })).toBeNull();
    expect(tokenAnywhere()).toBe(false);
  });

  it('says when the link is relative, and sends a chosen expiry', async () => {
    const f = host();
    f.on('POST', '/admin/v1/invites', { status: 201, body: { invite: invite(), token: TOKEN, link: `/invite#${TOKEN}` } });
    renderApp(f, { hash: '#/invites' });
    const form = await screen.findByRole('form', { name: 'New invite' });
    const u = userEvent.setup();
    await u.click(within(form).getByLabelText('At a date and time'));
    fireEvent.change(within(form).getByLabelText('Expiry date and time'), { target: { value: '2027-01-02T08:00' } });
    await u.click(within(form).getByRole('button', { name: 'Create invite' }));
    const panel = await screen.findByRole('region', { name: 'Invite link' });
    expect(panel.textContent).toContain('invites.link_base_url is empty');
    const body = f.callsTo('POST', '/admin/v1/invites')[0]!.body as { expires_at?: string };
    expect(body).toEqual({ expires_at: new Date('2027-01-02T08:00').toISOString() });
  });

  it('shows a refused expiry at the field', async () => {
    const f = host();
    f.on('POST', '/admin/v1/invites', errorReply(400, null, 'The expiry must be in the future.', { param: 'expires_at' }));
    renderApp(f, { hash: '#/invites' });
    const form = await screen.findByRole('form', { name: 'New invite' });
    const u = userEvent.setup();
    await u.click(within(form).getByLabelText('At a date and time'));
    fireEvent.change(within(form).getByLabelText('Expiry date and time'), { target: { value: '2020-01-02T08:00' } });
    await u.click(within(form).getByRole('button', { name: 'Create invite' }));
    expect((await within(form).findByRole('alert')).textContent).toContain('The invite was not created');
    expect(within(form).getByLabelText('Expiry date and time').getAttribute('aria-invalid')).toBe('true');
    expect(screen.queryByRole('region', { name: 'Invite link' })).toBeNull();
  });

  it('revokes a pending invite after a confirmation', async () => {
    const f = host();
    f.on('POST', '/admin/v1/invites/{invite_id}/revoke', { body: invite({ status: 'revoked' }) });
    renderApp(f, { hash: '#/invites' });
    const table = await screen.findByRole('table', { name: 'Invites, newest first' });
    const u = userEvent.setup();
    await u.click(within(table).getByRole('button', { name: 'Revoke invite For <script>alert(1)</script> Sam' }));
    expect(f.callsTo('POST', `/admin/v1/invites/${IID}/revoke`)).toHaveLength(0);
    const confirm = within(table).getByRole('group', { name: 'Revoke this invite?' });
    await u.click(within(confirm).getByRole('button', { name: 'Revoke invite' }));
    await waitFor(() => expect(within(table).getAllByRole('row')[1]!.textContent).toContain('Revoked'));
    expect(f.callsTo('POST', `/admin/v1/invites/${IID}/revoke`)).toHaveLength(1);
    expect(within(table).queryByRole('button', { name: /Revoke invite/ })).toBeNull();
  });

  it('shows a refused revoke', async () => {
    const f = host();
    f.on('POST', '/admin/v1/invites/{invite_id}/revoke', errorReply(409, 'conflict', 'Only a pending invite can be revoked; this one is expired.'));
    renderApp(f, { hash: '#/invites' });
    const table = await screen.findByRole('table', { name: 'Invites, newest first' });
    const u = userEvent.setup();
    await u.click(within(table).getByRole('button', { name: /Revoke invite/ }));
    await u.click(within(table).getByRole('button', { name: 'Revoke invite' }));
    expect((await within(table).findByRole('alert')).textContent).toContain('Only a pending invite can be revoked');
  });

  it('pages with the cursor and shows an empty list', async () => {
    const f = host();
    f.on('GET', '/admin/v1/invites', (c) =>
      c.query.get('cursor') === 'n2'
        ? { body: { data: [invite({ id: 'i9', note: 'Second page' })], next_cursor: null } }
        : { body: { data: [invite()], next_cursor: 'n2' } },
    );
    renderApp(f, { hash: '#/invites' });
    const u = userEvent.setup();
    await u.click(await screen.findByRole('button', { name: 'Show more' }));
    expect(await screen.findByText('Second page')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();
  });

  it('shows a list error', async () => {
    const f = host();
    f.on('GET', '/admin/v1/invites', errorReply(500, null, 'The host could not read its database.'));
    renderApp(f, { hash: '#/invites' });
    const alert = await screen.findByText('The invites could not be read');
    expect(alert.closest('[role=alert]')!.textContent).toContain('The host could not read its database.');
  });

  it('shows an empty list', async () => {
    const f = host();
    f.on('GET', '/admin/v1/invites', { body: { data: [], next_cursor: null } });
    renderApp(f, { hash: '#/invites' });
    expect(await screen.findByText('No invites yet.')).toBeTruthy();
  });
});
