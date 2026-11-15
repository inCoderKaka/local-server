import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { formatCount } from '@local-server/shared';
import { errorReply } from '@local-server/shared/test';
import { grantVersion, noGrant, signedIn } from '../test/fixtures';
import { renderApp } from '../test/render';

const XSS = '<img src=x onerror=alert(1)>';

describe('my limits', () => {
  it('shows the grant in plain language', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', {
      body: grantVersion(
        {
          models: [
            { model: 'example-model', context_limit: 4096, max_output_tokens: 1024 },
            { model: XSS, context_limit: null, max_output_tokens: null },
          ],
          quotas: [
            { metric: 'tokens', limit: 100000, window_seconds: 86400 },
            { metric: 'requests', limit: 500, window_seconds: 3600 },
          ],
          concurrency: 3,
          on_context_overflow: 'truncate',
          allowed_request_properties: ['background'],
          expires_at: '2026-12-31T00:00:00Z',
        },
        { version: 2, resource_request_id: '0192e5a0-0006-7000-8000-000000000006' },
      ),
    });
    const view = renderApp(f, { hash: '#/limits' });
    expect(await screen.findByRole('heading', { level: 1, name: 'My limits' })).toBeTruthy();
    const models = await screen.findByRole('table', { name: 'Models and their limits' });
    const rows = within(models).getAllByRole('row').slice(1);
    expect(rows[0]!.textContent).toContain(`${formatCount(4096)} tokens`);
    expect(rows[0]!.textContent).toContain(`${formatCount(1024)} tokens`);
    // A model name is text, never HTML; a limit the grant does not set is the host's default.
    expect(within(rows[1]!).getByText(XSS)).toBeTruthy();
    expect(view.container.querySelector('img')).toBeNull();
    expect(rows[1]!.textContent).toContain("The host's default");

    const quotas = screen.getByRole('table', { name: /How much you may use/ });
    expect(within(quotas).getAllByRole('row')[1]!.textContent).toBe(`Tokens${formatCount(100000)}24h`);
    expect(within(quotas).getAllByRole('row')[2]!.textContent).toBe('Requests5001h');

    const other = screen.getByRole('region', { name: 'Other terms' });
    expect(other.textContent).toContain('Up to 3 of your calls run at the same time');
    expect(other.textContent).toContain('It is cut to fit');
    expect(other.textContent).toContain('background');
    expect(other.textContent).toContain('No billing.');
    expect(other.textContent).toContain('Version 2');
    expect(within(other).getByRole('link', { name: 'your request' }).getAttribute('href')).toBe('#/requests/0192e5a0-0006-7000-8000-000000000006');
    expect(other.textContent).not.toContain('Never');
  });

  it('says there is no access yet and offers a request when the host set no grant (D-150)', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', noGrant);
    renderApp(f, { hash: '#/limits' });
    const card = await screen.findByRole('region', { name: 'No access yet' });
    expect(within(card).getByRole('link', { name: 'Ask the host for access' }).getAttribute('href')).toBe('#/requests');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows no quota and no models as such', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', { body: grantVersion({ models: [], quotas: [] }) });
    renderApp(f, { hash: '#/limits' });
    expect(await screen.findByText('No models: every call is refused.')).toBeTruthy();
    expect(screen.getByText('No quota: your use is not capped by a quota.')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Other terms' }).textContent).toContain('Never: it lasts until the host changes it.');
  });

  it("shows the host's error", async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', errorReply(500, 'internal_error', 'Something failed on the host.', { type: 'server_error' }));
    renderApp(f, { hash: '#/limits' });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Your limits could not be read');
    expect(alert.textContent).toContain('Something failed on the host.');
  });
});
