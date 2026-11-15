import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { fakeHost, resourceRequest, settingsList } from '../test/fixtures';
import { allPages, fixturePages, renderApp, settle } from '../test/render';

describe('pending resource requests count', () => {
  it('counts open requests across every page of the list', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/resource-requests', (c) =>
      c.query.get('cursor') === 'c1'
        ? { body: { data: [resourceRequest('r3')], next_cursor: null } }
        : { body: { data: [resourceRequest('r1'), resourceRequest('r2')], next_cursor: 'c1' } },
    );
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    const link = await screen.findByRole('link', { name: '3 pending resource requests' });
    expect(link.getAttribute('href')).toBe('#/requests');
    const calls = f.callsTo('GET', '/admin/v1/resource-requests');
    expect(calls.map((c) => [c.query.get('status'), c.query.get('cursor')])).toEqual([
      ['open', null],
      ['open', 'c1'],
    ]);
    // Consumer-written text is never shown in the shell.
    expect(document.body.textContent).not.toContain('Please');
  });

  it('opens the requests page', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/resource-requests', { body: { data: [resourceRequest('r1')], next_cursor: null } });
    renderApp(f, { pages: allPages, hash: '#/fake-a' });
    await userEvent.setup().click(await screen.findByRole('link', { name: '1 pending resource request' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Requests' })).toBeTruthy();
  });

  it('tolerates 501 with no error, keeps polling and shows the count once the host has requests', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: '5s' } }) } });
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    await settle();
    expect(screen.getByRole('heading', { name: 'Fake page A' })).toBeTruthy();
    expect(f.callsTo('GET', '/admin/v1/resource-requests')).toHaveLength(1);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText(/pending resource/)).toBeNull();
    expect(screen.queryByText(/Requests/)).toBeNull();

    await vi.advanceTimersByTimeAsync(5000);
    await settle();
    expect(f.callsTo('GET', '/admin/v1/resource-requests')).toHaveLength(2);
    expect(screen.queryByRole('alert')).toBeNull();

    f.on('GET', '/admin/v1/resource-requests', { body: { data: [resourceRequest('r1'), resourceRequest('r2')], next_cursor: null } });
    await vi.advanceTimersByTimeAsync(5000);
    await settle();
    expect(screen.getByRole('link', { name: '2 pending resource requests' })).toBeTruthy();
  });

  it('shows n/a, not a banner, when the count cannot be read', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/resource-requests', errorReply(500, null, 'Something failed on the host.', { type: 'server_error' }));
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    const na = await screen.findByText('Requests: n/a');
    expect(na.parentElement?.getAttribute('title')).toBe('Something failed on the host.');
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
