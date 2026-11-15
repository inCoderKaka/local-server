import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { fakeHost, hostInfo, settingsList } from '../test/fixtures';
import { allPages, renderApp } from '../test/render';

async function strip() {
  const region = await screen.findByRole('region', { name: 'Host resources' });
  await within(region).findByText('Host total');
  return region;
}

function valueOf(region: HTMLElement, label: string): string {
  const dt = within(region).getByText(label);
  return dt.nextElementSibling?.textContent ?? '';
}

describe('layout on every page', () => {
  it('shows host total, share budget and other apps on the dashboard', async () => {
    renderApp(fakeHost());
    await screen.findByRole('heading', { name: 'Dashboard' });
    const r = await strip();
    expect(valueOf(r, 'Host total')).toBe('Memory 16.0 GiB · CPU 12.5%');
    expect(valueOf(r, 'Share-budget slice')).toBe('Memory 8.0 GiB · CPU 6.3%');
    expect(valueOf(r, 'Other apps')).toBe('Memory 4.0 GiB · CPU 3.5%');
    expect(valueOf(r, 'Memory budget')).toBe('24.0 GiB');
    // Each value names how it was measured.
    expect(within(r).getByText('Memory 16.0 GiB').getAttribute('title')).toBe('Memory, measured by os-total-counter');
    expect(within(r).getByText('CPU 3.5%').getAttribute('title')).toBe('CPU, measured by total-minus-slice');
  });

  it('shows the same strip on another registered page', async () => {
    renderApp(fakeHost(), { pages: allPages, hash: '#/fake-a' });
    expect(await screen.findByRole('heading', { name: 'Fake page A' })).toBeTruthy();
    const r = await strip();
    expect(valueOf(r, 'Host total')).toBe('Memory 16.0 GiB · CPU 12.5%');
    expect(valueOf(r, 'Share-budget slice')).toBe('Memory 8.0 GiB · CPU 6.3%');
    expect(valueOf(r, 'Other apps')).toBe('Memory 4.0 GiB · CPU 3.5%');
    expect(valueOf(r, 'Memory budget')).toBe('24.0 GiB');
    expect(await screen.findByText('Sharing')).toBeTruthy();
    expect(document.title).toBe('Fake A · local-server admin');
  });

  it('shows a value that cannot be read as n/a, never 0', async () => {
    renderApp(fakeHost());
    const r = await strip();
    expect(valueOf(r, 'Unattributed')).toBe('Memory n/a · CPU n/a');
    expect(within(r).getByText('Memory n/a').getAttribute('title')).toBe('Memory, measured by access-denied');
  });

  it('shows no invented budget when the setting is missing or unreadable, and no limit for 0', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({}, ['scheduler.memory_budget_bytes']) } });
    const { unmount } = renderApp(f);
    let r = await strip();
    expect(within(r).queryByText('Memory budget')).toBeNull();
    unmount();

    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'scheduler.memory_budget_bytes': { value: 'lots' } }) } });
    const second = renderApp(f);
    r = await strip();
    expect(within(r).queryByText('Memory budget')).toBeNull();
    second.unmount();

    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'scheduler.memory_budget_bytes': { value: 0 } }) } });
    renderApp(f);
    r = await strip();
    expect(await within(r).findByText('Memory budget')).toBeTruthy();
    expect(valueOf(r, 'Memory budget')).toBe('no limit');
  });

  it('lists registered pages in the navigation by order, without hidden ones', async () => {
    renderApp(fakeHost(), { pages: allPages });
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    const links = within(nav).getAllByRole('link').map((a) => [a.textContent, a.getAttribute('href')]);
    // Real pages sit between them by their order; only the fixture pages and the dashboard are checked.
    const known = ['Dashboard', 'Fake A', 'Fake B', 'Fake C'];
    expect(links.filter(([label]) => known.includes(label ?? ''))).toEqual([
      ['Dashboard', '#/'],
      ['Fake B', '#/fake-b'],
      ['Fake A', '#/fake-a'],
    ]);
    expect(within(nav).getByRole('link', { name: 'Dashboard' }).getAttribute('aria-current')).toBe('page');
  });

  it('passes route params to a page that is not in the navigation', async () => {
    renderApp(fakeHost(), { pages: allPages, hash: '#/fake-c/item%2042' });
    expect(await screen.findByRole('heading', { name: 'Fake detail item 42' })).toBeTruthy();
  });

  it('shows the not-found page for an unknown route, inside the layout', async () => {
    renderApp(fakeHost(), { hash: '#/no-such-page' });
    expect(await screen.findByText('Page not found')).toBeTruthy();
    expect(await strip()).toBeTruthy();
    await userEvent.setup().click(screen.getByRole('link', { name: 'Go to the dashboard' }));
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeTruthy();
  });

  it('shows the sharing state badge', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/host', { body: hostInfo({ sharing_state: 'paused', paused_at: '2026-01-15T09:00:00Z' }) });
    renderApp(f, { pages: allPages, hash: '#/fake-a' });
    const badge = await screen.findByText('Sharing state:');
    expect(badge.parentElement?.textContent).toBe('Sharing state: Paused');
  });
});
