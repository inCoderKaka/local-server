import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { pages } from '../registry';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';
import { accessKey, CID, consumer } from './testData';

const XSS = '<img src=x onerror=alert(1)>';

function hostWith(over: Parameters<typeof consumer>[0] = {}) {
  const f = fakeHost();
  f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer(over) });
  f.on('GET', '/admin/v1/consumers/{consumer_id}/access-keys', { body: { data: [accessKey()] } });
  return f;
}

describe('consumer pages', () => {
  it('are registered once each, the detail page out of the navigation', async () => {
    const ids = pages.map((p) => p.id);
    expect(ids).toEqual(expect.arrayContaining(['consumers', 'invites', 'consumer']));
    expect(pages.find((p) => p.id === 'consumer')).toMatchObject({ path: '/consumers/:consumerId', navVisible: false });
    renderApp(fakeHost());
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    expect(within(nav).getByRole('link', { name: 'Consumers' }).getAttribute('href')).toBe('#/consumers');
    expect(within(nav).getByRole('link', { name: 'Invites' }).getAttribute('href')).toBe('#/invites');
    expect(within(nav).queryByRole('link', { name: 'Consumer' })).toBeNull();
  });

  it('lists consumers and renders a name with markup as text', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers', {
      body: {
        data: [consumer({ display_name: XSS }), consumer({ id: 'c2', kind: 'individual', display_name: 'Pat', has_login: true, status: 'suspended' })],
        next_cursor: null,
      },
    });
    renderApp(f, { hash: '#/consumers' });
    const table = await screen.findByRole('table', { name: 'Consumers, newest first' });
    const link = within(table).getByRole('link', { name: XSS });
    expect(link.getAttribute('href')).toBe(`#/consumers/${CID}`);
    expect(link.textContent).toBe(XSS);
    expect(document.querySelector('img')).toBeNull();
    const rows = within(table).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('Key only');
    expect(rows[2]!.textContent).toContain('Suspended');
    expect(rows[2]!.textContent).toContain('Password or passkey');
    // The page size is the server's: no limit is sent, and deleted consumers are not asked for.
    const q = f.callsTo('GET', '/admin/v1/consumers')[0]!.query;
    expect([...q.keys()]).toEqual([]);
  });

  it('pages with the cursor and filters by status', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers', (c) => {
      if (c.query.get('status') === 'deleted') return { body: { data: [consumer({ id: 'd1', status: 'deleted', display_name: null })], next_cursor: null } };
      if (c.query.get('cursor') === 'p2') return { body: { data: [consumer({ id: 'c2', display_name: 'Second' })], next_cursor: null } };
      return { body: { data: [consumer()], next_cursor: 'p2' } };
    });
    renderApp(f, { hash: '#/consumers' });
    const u = userEvent.setup();
    await u.click(await screen.findByRole('button', { name: 'Show more' }));
    expect(await screen.findByRole('link', { name: 'Second' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Example App' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();
    await u.selectOptions(screen.getByLabelText('Show'), 'deleted');
    expect(await screen.findByText('No name')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Example App' })).toBeNull();
    expect(f.callsTo('GET', '/admin/v1/consumers').at(-1)!.query.get('status')).toBe('deleted');
  });

  it('shows an empty list', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers', { body: { data: [], next_cursor: null } });
    renderApp(f, { hash: '#/consumers' });
    expect(await screen.findByText('No consumers to show.')).toBeTruthy();
  });

  it('shows a list error', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers', errorReply(500, null, 'The host could not read its database.'));
    renderApp(f, { hash: '#/consumers' });
    const alert = await screen.findByText('The consumers could not be read');
    expect(alert.closest('[role=alert]')!.textContent).toContain('The host could not read its database.');
  });

  it('creates a key-only consumer and opens its page', async () => {
    const f = hostWith({ display_name: 'New App' });
    f.on('GET', '/admin/v1/consumers', { body: { data: [], next_cursor: null } });
    f.on('POST', '/admin/v1/consumers', { status: 201, body: consumer({ display_name: 'New App' }) });
    renderApp(f, { hash: '#/consumers' });
    const form = await screen.findByRole('form', { name: 'New key-only consumer' });
    const u = userEvent.setup();
    await u.click(within(form).getByRole('button', { name: 'Create consumer' }));
    expect(within(form).getByText('Choose a kind.')).toBeTruthy();
    expect(within(form).getByText('Give a display name.')).toBeTruthy();
    expect(f.callsTo('POST', '/admin/v1/consumers')).toHaveLength(0);

    await u.selectOptions(within(form).getByLabelText('Kind'), 'application');
    await u.type(within(form).getByLabelText('Display name'), ' New App ');
    await u.click(within(form).getByRole('button', { name: 'Create consumer' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'New App' })).toBeTruthy();
    expect(window.location.hash).toBe(`#/consumers/${CID}`);
    expect(f.callsTo('POST', '/admin/v1/consumers')[0]!.body).toEqual({ kind: 'application', display_name: 'New App' });
  });

  it('shows a refused field at the field', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers', { body: { data: [], next_cursor: null } });
    f.on('POST', '/admin/v1/consumers', errorReply(400, null, 'The email address is not valid.', { param: 'email' }));
    renderApp(f, { hash: '#/consumers' });
    const form = await screen.findByRole('form', { name: 'New key-only consumer' });
    const u = userEvent.setup();
    await u.selectOptions(within(form).getByLabelText('Kind'), 'individual');
    await u.type(within(form).getByLabelText('Display name'), 'Pat');
    await u.type(within(form).getByLabelText('Email (optional)'), 'not-an-email');
    await u.click(within(form).getByRole('button', { name: 'Create consumer' }));
    expect((await within(form).findByRole('alert')).textContent).toMatch(/Request id: req-\d+/);
    expect(within(form).getByLabelText('Email (optional)').getAttribute('aria-invalid')).toBe('true');
    expect(within(form).getByLabelText('Display name').getAttribute('aria-invalid')).toBeNull();
    expect(f.callsTo('POST', '/admin/v1/consumers')[0]!.body).toEqual({ kind: 'individual', display_name: 'Pat', email: 'not-an-email' });
  });

  it('shows a consumer with markup in its fields as text', async () => {
    renderApp(hostWith({ display_name: XSS, email: XSS, username: XSS }), { hash: `#/consumers/${CID}` });
    expect(await screen.findByRole('heading', { level: 1, name: XSS })).toBeTruthy();
    const details = screen.getByRole('region', { name: 'Details' });
    expect(within(details).getAllByText(XSS)).toHaveLength(2);
    expect(within(details).getByText('None: this consumer uses access keys only')).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
  });

  it('shows a consumer that does not exist', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/consumers/{consumer_id}', errorReply(404, 'not_found', 'No such consumer.'));
    renderApp(f, { hash: '#/consumers/nope' });
    const alert = await screen.findByText('The consumer could not be read');
    expect(alert.closest('[role=alert]')!.textContent).toContain('No such consumer.');
  });

  it('suspends and reactivates a consumer', async () => {
    const f = hostWith();
    f.on('POST', '/admin/v1/consumers/{consumer_id}/suspend', { body: consumer({ status: 'suspended' }) });
    f.on('POST', '/admin/v1/consumers/{consumer_id}/reactivate', { body: consumer() });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const state = await screen.findByRole('region', { name: 'State' });
    const u = userEvent.setup();
    await u.click(within(state).getByRole('button', { name: 'Suspend' }));
    expect(await within(state).findByRole('button', { name: 'Reactivate' })).toBeTruthy();
    expect(within(screen.getByRole('region', { name: 'Details' })).getByText('Suspended')).toBeTruthy();
    expect(screen.getByText(/every one of its keys is refused/)).toBeTruthy();
    expect(f.callsTo('POST', `/admin/v1/consumers/${CID}/suspend`)).toHaveLength(1);
    await u.click(within(state).getByRole('button', { name: 'Reactivate' }));
    expect(await within(state).findByRole('button', { name: 'Suspend' })).toBeTruthy();
    expect(f.callsTo('POST', `/admin/v1/consumers/${CID}/reactivate`)).toHaveLength(1);
  });

  it('shows a refused state change', async () => {
    const f = hostWith({ status: 'suspended' });
    f.on('POST', '/admin/v1/consumers/{consumer_id}/reactivate', errorReply(409, 'conflict', 'A deleted consumer cannot be reactivated.'));
    renderApp(f, { hash: `#/consumers/${CID}` });
    const state = await screen.findByRole('region', { name: 'State' });
    await userEvent.setup().click(within(state).getByRole('button', { name: 'Reactivate' }));
    expect((await within(state).findByRole('alert')).textContent).toContain('A deleted consumer cannot be reactivated.');
  });

  it('deletes a consumer only after a confirmation that says what is erased and what stays', async () => {
    const f = hostWith();
    f.on('DELETE', '/admin/v1/consumers/{consumer_id}', () => {
      f.on('GET', '/admin/v1/consumers/{consumer_id}', {
        body: consumer({ status: 'deleted', display_name: null, email: null, deleted_at: '2026-01-16T10:00:00Z' }),
      });
      f.on('GET', '/admin/v1/consumers/{consumer_id}/access-keys', {
        body: { data: [accessKey({ status: 'revoked', name: null, revoked_at: '2026-01-16T10:00:00Z' })] },
      });
      return { status: 204 };
    });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const state = await screen.findByRole('region', { name: 'State' });
    const u = userEvent.setup();

    await u.click(within(state).getByRole('button', { name: 'Delete consumer' }));
    let confirm = within(state).getByRole('group', { name: 'Delete this consumer?' });
    expect(confirm.textContent).toContain('display name, email, login, key names and invite notes are erased');
    expect(confirm.textContent).toContain('access keys is revoked');
    expect(confirm.textContent).toContain('usage records stay');
    await u.click(within(confirm).getByRole('button', { name: 'Cancel' }));
    expect(f.callsTo('DELETE', `/admin/v1/consumers/${CID}`)).toHaveLength(0);

    await u.click(within(state).getByRole('button', { name: 'Delete consumer' }));
    confirm = within(state).getByRole('group', { name: 'Delete this consumer?' });
    await u.click(within(confirm).getByRole('button', { name: 'Delete for good' }));
    expect(await within(state).findByText(/This consumer is deleted/)).toBeTruthy();
    expect(f.callsTo('DELETE', `/admin/v1/consumers/${CID}`)).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('No name');
    expect(within(state).queryByRole('button', { name: 'Reactivate' })).toBeNull();
    const keys = screen.getByRole('region', { name: 'Access keys' });
    await waitFor(() => expect(within(keys).getAllByRole('row')[1]!.textContent).toContain('Revoked'));
    // A deleted consumer gets no new key.
    expect(within(keys).queryByRole('form', { name: 'Issue an access key' })).toBeNull();
  });

  it('shows a refused deletion inside the confirmation', async () => {
    const f = hostWith();
    f.on('DELETE', '/admin/v1/consumers/{consumer_id}', errorReply(500, null, 'The host could not write its database.'));
    renderApp(f, { hash: `#/consumers/${CID}` });
    const state = await screen.findByRole('region', { name: 'State' });
    const u = userEvent.setup();
    await u.click(within(state).getByRole('button', { name: 'Delete consumer' }));
    const confirm = within(state).getByRole('group', { name: 'Delete this consumer?' });
    await u.click(within(confirm).getByRole('button', { name: 'Delete for good' }));
    expect((await within(confirm).findByRole('alert')).textContent).toContain('The host could not write its database.');
    expect(within(confirm).getByRole('button', { name: 'Delete for good' }).hasAttribute('disabled')).toBe(false);
  });
});
