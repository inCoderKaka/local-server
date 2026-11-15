import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { formatCount } from '@local-server/shared';
import { errorReply } from '@local-server/shared/test';
import { grantVersion, noGrant, resourceRequest, rollup, signedIn } from '../test/fixtures';
import { renderApp } from '../test/render';

describe('the start page', () => {
  it('sums up the account and links to the pages', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', {
      body: grantVersion({ models: [{ model: 'example-model' }, { model: 'second-model' }], concurrency: 2 }),
    });
    f.on('GET', '/v1/usage', { body: { data: [rollup({ granularity: 'day', total_tokens: 2500 })], next_cursor: null } });
    f.on('GET', '/v1/resource-requests', (c) => ({
      body: { data: c.query.get('status') === 'open' ? [resourceRequest(), resourceRequest({ id: 'r2' })] : [], next_cursor: null },
    }));
    renderApp(f);
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Your account' }).textContent).toContain('You are logged in as Example Consumer.');
    const limits = screen.getByRole('region', { name: 'Your limits' });
    await waitFor(() => expect(limits.textContent).toContain('You may use 2 models (example-model, second-model), 2 requests at once.'));
    expect(within(limits).getByRole('link', { name: 'My limits' }).getAttribute('href')).toBe('#/limits');
    const used = screen.getByRole('region', { name: 'Used now' });
    await waitFor(() => expect(used.textContent).toContain(`${formatCount(2500)} of ${formatCount(100000)} tokens used`));
    expect(within(used).getByRole('link', { name: 'My usage' }).getAttribute('href')).toBe('#/usage');
    const requests = screen.getByRole('region', { name: 'Requests' });
    await waitFor(() => expect(requests.textContent).toContain('You have 2 pending requests.'));
    expect(within(requests).getByRole('link', { name: 'Requests' }).getAttribute('href')).toBe('#/requests');
    // Every page is in the navigation.
    const nav = screen.getByRole('navigation', { name: 'Main' });
    expect(within(nav).getAllByRole('link').map((l) => l.textContent)).toEqual(['Home', 'My limits', 'My usage', 'My keys', 'Requests', 'Settings']);
  });

  it('says there is no access yet without a grant (D-150)', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', noGrant);
    renderApp(f);
    const limits = await screen.findByRole('region', { name: 'Your limits' });
    await waitFor(() => expect(limits.textContent).toContain('The host has not given you access yet.'));
    expect(within(limits).getByRole('link', { name: 'Ask for access' }).getAttribute('href')).toBe('#/requests');
    expect(await screen.findByText('You have no pending requests.')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows what could not be read', async () => {
    const f = signedIn();
    f.on('GET', '/v1/resource-requests', errorReply(500, 'internal_error', 'Something failed.', { type: 'server_error' }));
    renderApp(f);
    const requests = await screen.findByRole('region', { name: 'Requests' });
    expect((await within(requests).findByRole('alert')).textContent).toContain('Your requests could not be read');
  });
});
