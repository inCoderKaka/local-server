import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';
import { ShownOnce } from './ShownOnce';
import { accessKey, CID, consumer, KID } from './testData';

// A made-up secret: the tests check where it shows and that it goes, never snapshot it.
const SECRET = 'ls_newkey12_' + 'S'.repeat(32);

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/consumers', { body: { data: [consumer()], next_cursor: null } });
  f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer() });
  f.on('GET', '/admin/v1/consumers/{consumer_id}/access-keys', {
    body: {
      data: [
        accessKey(),
        accessKey({ id: 'k2', prefix: 'oldkey99', name: '<b>old</b>', status: 'revoked', revoked_at: '2026-01-15T11:00:00Z' }),
        accessKey({ id: 'k3', prefix: 'bound000', name: null, bound_node: 'nodeid123', bound_node_name: 'laptop', expires_at: '2027-01-01T00:00:00Z' }),
      ],
    },
  });
  return f;
}

async function keysCard() {
  return screen.findByRole('region', { name: 'Access keys' });
}

function secretAnywhere(): boolean {
  const inputs = [...document.querySelectorAll('input')].map((i) => i.value);
  return (
    document.body.textContent!.includes(SECRET) ||
    inputs.includes(SECRET) ||
    window.location.href.includes(SECRET) ||
    Object.values({ ...localStorage }).some((v) => v.includes(SECRET)) ||
    Object.values({ ...sessionStorage }).some((v) => v.includes(SECRET))
  );
}

describe('access keys', () => {
  it('lists keys by prefix with status, node and expiry', async () => {
    renderApp(host(), { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    const table = await within(card).findByRole('table', { name: 'Access keys, by prefix' });
    const rows = within(table).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('ls_abcd1234_…');
    expect(rows[1]!.textContent).toContain('Active');
    expect(rows[1]!.textContent).toContain('Any node');
    expect(rows[1]!.textContent).toContain('Never');
    expect(rows[2]!.textContent).toContain('<b>old</b>');
    expect(rows[2]!.querySelector('b')).toBeNull();
    expect(within(rows[2]!).queryByRole('button', { name: /Revoke/ })).toBeNull();
    expect(rows[3]!.textContent).toContain('laptop');
    expect(rows[3]!.textContent).not.toContain('Never');
  });

  it('issues a key and shows its secret once, gone after dismissing', async () => {
    const f = host();
    f.on('POST', '/admin/v1/consumers/{consumer_id}/access-keys', {
      status: 201,
      body: { access_key: accessKey({ id: 'k9', prefix: 'newkey12', name: 'CI' }), secret: SECRET },
    });
    const log = vi.spyOn(console, 'log');
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    const form = within(card).getByRole('form', { name: 'Issue an access key' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Key name (optional)'), 'CI');
    await u.click(within(form).getByRole('button', { name: 'Issue key' }));

    const panel = await within(card).findByRole('region', { name: 'New access key' });
    expect((within(panel).getByLabelText('Access key secret') as HTMLInputElement).value).toBe(SECRET);
    expect(panel.textContent).toContain('shown only this once');
    expect(f.callsTo('POST', `/admin/v1/consumers/${CID}/access-keys`)[0]!.body).toEqual({ name: 'CI' });
    // The list is read again after the issue.
    expect(f.callsTo('GET', `/admin/v1/consumers/${CID}/access-keys`).length).toBeGreaterThanOrEqual(2);
    expect(window.location.href).not.toContain(SECRET);
    expect(localStorage.length + sessionStorage.length).toBe(0);

    await u.click(within(panel).getByRole('button', { name: 'Copy' }));
    expect(await within(panel).findByText('Copied.')).toBeTruthy();
    expect(await navigator.clipboard.readText()).toBe(SECRET);

    await u.click(within(panel).getByRole('button', { name: 'I have copied it' }));
    expect(within(card).queryByRole('region', { name: 'New access key' })).toBeNull();
    expect(secretAnywhere()).toBe(false);
    expect(log.mock.calls.flat().some((a) => String(a).includes(SECRET))).toBe(false);
    log.mockRestore();
  });

  it('forgets the secret when the host leaves the page', async () => {
    const f = host();
    f.on('POST', '/admin/v1/consumers/{consumer_id}/access-keys', { status: 201, body: { access_key: accessKey({ prefix: 'newkey12' }), secret: SECRET } });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    const u = userEvent.setup();
    await u.click(within(card).getByRole('button', { name: 'Issue key' }));
    await within(card).findByRole('region', { name: 'New access key' });
    expect(secretAnywhere()).toBe(true);

    window.location.hash = '#/consumers';
    await screen.findByRole('table', { name: 'Consumers, newest first' });
    window.location.hash = `#/consumers/${CID}`;
    await keysCard();
    expect(screen.queryByRole('region', { name: 'New access key' })).toBeNull();
    expect(secretAnywhere()).toBe(false);
  });

  it('sends a chosen expiry and a bound node, and asks for a missing date', async () => {
    const f = host();
    f.on('POST', '/admin/v1/consumers/{consumer_id}/access-keys', { status: 201, body: { access_key: accessKey(), secret: SECRET } });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    const form = within(card).getByRole('form', { name: 'Issue an access key' });
    const u = userEvent.setup();
    await u.click(within(form).getByLabelText('At a date and time'));
    await u.click(within(form).getByRole('button', { name: 'Issue key' }));
    expect(within(form).getByText('Choose a date and time.')).toBeTruthy();
    expect(f.callsTo('POST', `/admin/v1/consumers/${CID}/access-keys`)).toHaveLength(0);

    fireEvent.change(within(form).getByLabelText('Expiry date and time'), { target: { value: '2027-03-01T09:30' } });
    await u.type(within(form).getByLabelText('Bind to a tailnet node (optional)'), 'laptop');
    await u.click(within(form).getByRole('button', { name: 'Issue key' }));
    await within(card).findByRole('region', { name: 'New access key' });
    expect(f.callsTo('POST', `/admin/v1/consumers/${CID}/access-keys`)[0]!.body).toEqual({
      expires_at: new Date('2027-03-01T09:30').toISOString(),
      bound_node: 'laptop',
    });
  });

  it('shows a refused field at the field and no secret', async () => {
    const f = host();
    f.on('POST', '/admin/v1/consumers/{consumer_id}/access-keys', errorReply(400, null, 'No node of this tailnet has that address, node ID or name.', { param: 'bound_node' }));
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    const form = within(card).getByRole('form', { name: 'Issue an access key' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Bind to a tailnet node (optional)'), 'nowhere');
    await u.click(within(form).getByRole('button', { name: 'Issue key' }));
    expect((await within(form).findByRole('alert')).textContent).toContain('The key was not issued');
    const node = within(form).getByLabelText('Bind to a tailnet node (optional)');
    expect(node.getAttribute('aria-invalid')).toBe('true');
    expect(within(card).queryByRole('region', { name: 'New access key' })).toBeNull();
  });

  it('revokes a key after a confirmation', async () => {
    const f = host();
    f.on('POST', '/admin/v1/access-keys/{access_key_id}/revoke', {
      body: accessKey({ status: 'revoked', revoked_at: '2026-01-16T10:00:00Z' }),
    });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    await within(card).findByRole('table', { name: 'Access keys, by prefix' });
    const u = userEvent.setup();
    await u.click(within(card).getByRole('button', { name: 'Revoke key ls_abcd1234_…' }));
    expect(f.callsTo('POST', `/admin/v1/access-keys/${KID}/revoke`)).toHaveLength(0);
    const confirm = within(card).getByRole('group', { name: 'Revoke key ls_abcd1234_…?' });
    await u.click(within(confirm).getByRole('button', { name: 'Revoke key' }));
    await waitFor(() => expect(within(card).queryByRole('button', { name: 'Revoke key ls_abcd1234_…' })).toBeNull());
    expect(f.callsTo('POST', `/admin/v1/access-keys/${KID}/revoke`)).toHaveLength(1);
    const row = within(card).getAllByRole('row')[1]!;
    expect(row.textContent).toContain('Revoked');
  });

  it('shows a refused revoke', async () => {
    const f = host();
    f.on('POST', '/admin/v1/access-keys/{access_key_id}/revoke', errorReply(404, 'not_found', 'No such access key.'));
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    await within(card).findByRole('table', { name: 'Access keys, by prefix' });
    const u = userEvent.setup();
    await u.click(within(card).getByRole('button', { name: 'Revoke key ls_abcd1234_…' }));
    const confirm = within(card).getByRole('group', { name: 'Revoke key ls_abcd1234_…?' });
    await u.click(within(confirm).getByRole('button', { name: 'Revoke key' }));
    expect((await within(confirm).findByRole('alert')).textContent).toContain('No such access key.');
  });

  it('shows a key list error', async () => {
    const f = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}/access-keys', errorReply(500, null, 'The host could not read its database.'));
    renderApp(f, { hash: `#/consumers/${CID}` });
    const card = await keysCard();
    expect((await within(card).findByRole('alert')).textContent).toContain('The access keys could not be read');
  });
});

describe('a value shown once', () => {
  const original = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
  afterEach(() => {
    if (original) Object.defineProperty(navigator, 'clipboard', original);
    else delete (navigator as { clipboard?: unknown }).clipboard;
  });

  it('says to copy by hand when the browser gives no clipboard', () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true });
    const done = vi.fn();
    render(
      <ShownOnce title="T" label="Value" value="v" onDismiss={done}>
        Why
      </ShownOnce>,
    );
    expect(screen.queryByRole('button', { name: 'Copy' })).toBeNull();
    expect(screen.getByText(/select the text and copy it/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'I have copied it' }));
    expect(done).toHaveBeenCalledOnce();
  });
});
