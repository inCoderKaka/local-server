import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { accessKey, signedIn } from '../test/fixtures';
import { renderApp } from '../test/render';

const XSS = '<img src=x onerror=alert(1)>';

describe('my keys', () => {
  it('lists the keys by prefix with label, status, expiry and bound device, read only', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/access-keys', {
      body: {
        data: [
          accessKey(),
          accessKey({ id: 'k2', prefix: 'ffff0000', name: XSS, status: 'revoked', bound_node: 'nLAPTOP', bound_node_name: 'laptop', expires_at: '2026-06-01T00:00:00Z' }),
          accessKey({ id: 'k3', prefix: 'eeee1111', name: null, status: 'expired', bound_node: 'nPHONE', bound_node_name: null }),
        ],
      },
    });
    const view = renderApp(f, { hash: '#/keys' });
    const table = await screen.findByRole('table', { name: 'Your access keys' });
    const rows = within(table).getAllByRole('row').slice(1);
    expect(within(rows[0]!).getAllByRole('cell').map((c) => c.textContent)).toEqual([
      'Laptop key',
      'ls_abcd1234_…',
      'Active',
      expect.any(String),
      'Never',
      'Any device',
    ]);
    expect(within(rows[1]!).getByText(XSS)).toBeTruthy();
    expect(view.container.querySelector('img')).toBeNull();
    expect(rows[1]!.textContent).toContain('Revoked');
    expect(rows[1]!.textContent).toContain('Only from laptop');
    expect(rows[1]!.textContent).not.toContain('Never');
    expect(rows[2]!.textContent).toContain('No label');
    expect(rows[2]!.textContent).toContain('Only from nPHONE');
    // Read only: the page has no way to issue, change or revoke a key.
    expect(screen.queryByRole('button', { name: /issue|revoke|delete/i })).toBeNull();
    expect(f.calls.every((c) => c.method === 'GET')).toBe(true);
  });

  it('says when there are no keys', async () => {
    renderApp(signedIn(), { hash: '#/keys' });
    expect(await screen.findByText('You have no access keys yet.')).toBeTruthy();
  });

  it("shows the host's error", async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/access-keys', errorReply(503, 'unavailable', 'The host is busy.'));
    renderApp(f, { hash: '#/keys' });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Your keys could not be read');
    expect(alert.textContent).toContain('The host is busy.');
  });
});
