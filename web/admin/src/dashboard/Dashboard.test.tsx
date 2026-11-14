import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { fakeHost, hostInfo, sample, settingsList } from '../test/fixtures';
import { renderApp } from '../test/render';

async function dashboard() {
  await screen.findByRole('heading', { name: 'Dashboard' });
}

describe('dashboard', () => {
  it('shows the current use of every part with how it was measured', async () => {
    renderApp(fakeHost());
    await dashboard();
    const card = await screen.findByRole('region', { name: 'Host use now' });
    const row = (name: string) => within(card).getByRole('rowheader', { name }).closest('tr')!;
    expect(row('Host total').textContent).toContain('16.0 GiB');
    expect(row('Share-budget slice').textContent).toContain('8.0 GiB');
    expect(row('Other apps').textContent).toContain('4.0 GiB');
    expect(row('Unattributed').textContent).toContain('n/a');
    expect(row('Unattributed').textContent).toContain('measured by access-denied');
    expect(within(card).getByRole('meter', { name: 'Share-budget slice memory' }).getAttribute('value')).toBe(String(8 * 1024 ** 3));
    expect(within(card).queryByRole('meter', { name: 'Unattributed memory' })).toBeNull();
    expect(within(card).getByText(/Memory budget: 24.0 GiB/)).toBeTruthy();
  });

  it('pauses and resumes sharing with the switch', async () => {
    const f = fakeHost();
    f.on('POST', '/admin/v1/host/pause', { body: hostInfo({ sharing_state: 'paused', paused_at: '2026-01-15T09:00:00Z' }) });
    f.on('POST', '/admin/v1/host/resume', { body: hostInfo() });
    renderApp(f);
    await dashboard();
    const u = userEvent.setup();
    const sw = await screen.findByRole('switch', { name: 'Share this host with consumers' });
    expect(sw.getAttribute('aria-checked')).toBe('true');
    await u.click(sw);
    expect(f.callsTo('POST', '/admin/v1/host/pause')).toHaveLength(1);
    expect(f.callsTo('POST', '/admin/v1/host/resume')).toHaveLength(0);
    const card = screen.getByRole('region', { name: 'Sharing' });
    await within(card).findByText(/since/);
    expect(sw.getAttribute('aria-checked')).toBe('false');
    expect(within(card).getByText('Paused')).toBeTruthy();
    expect(screen.getByText('Sharing state:').parentElement?.textContent).toBe('Sharing state: Paused');
    await u.click(sw);
    expect(f.callsTo('POST', '/admin/v1/host/resume')).toHaveLength(1);
    expect(f.callsTo('POST', '/admin/v1/host/pause')).toHaveLength(1);
    await waitFor(() => expect(sw.getAttribute('aria-checked')).toBe('true'));
    expect(card.querySelector('strong')?.textContent).toBe('Sharing');
  });

  it('shows when quiet hours end', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/host', { body: hostInfo({ sharing_state: 'quiet', sharing_resumes_at: '2026-01-16T07:00:00Z' }) });
    renderApp(f);
    await dashboard();
    const card = await screen.findByRole('region', { name: 'Sharing' });
    expect(await within(card).findByText('Quiet hours')).toBeTruthy();
    expect(within(card).getByText(/until/)).toBeTruthy();
  });

  it('always warns that sleep cannot be blocked', async () => {
    const f = fakeHost();
    renderApp(f);
    await dashboard();
    const note = await screen.findByRole('note');
    expect(note.textContent).toContain('Closing the lid or putting this machine to sleep cannot be blocked');
    expect(note.textContent).toContain('stops serving');
  });

  it('saves the quiet hours', async () => {
    const f = fakeHost();
    const after = settingsList({ 'host.quiet_hours.enabled': { value: true }, 'host.quiet_hours.start': { value: '23:00' } });
    f.on('PATCH', '/admin/v1/settings', { body: { data: after } });
    renderApp(f);
    await dashboard();
    const form = await screen.findByRole('form', { name: 'Quiet hours' });
    const u = userEvent.setup();
    await u.click(within(form).getByLabelText('Use quiet hours'));
    await u.clear(within(form).getByLabelText('Start'));
    await u.type(within(form).getByLabelText('Start'), '23:00');
    await u.click(within(form).getByRole('button', { name: 'Save quiet hours' }));
    expect(await within(form).findByText('Saved.')).toBeTruthy();
    expect(f.callsTo('PATCH', '/admin/v1/settings')[0]?.body).toEqual({
      values: {
        'host.quiet_hours.enabled': true,
        'host.quiet_hours.time_zone': 'Local',
        'host.quiet_hours.start': '23:00',
        'host.quiet_hours.end': '07:00',
      },
    });
    // The host state is read again: quiet hours may have started.
    expect(f.callsTo('GET', '/admin/v1/host').length).toBeGreaterThanOrEqual(2);
  });

  it('shows the server refusing a quiet-hours value', async () => {
    const f = fakeHost();
    f.on('PATCH', '/admin/v1/settings', errorReply(400, null, 'The host refuses this value for setting host.quiet_hours.start.', { param: 'host.quiet_hours.start' }));
    renderApp(f);
    await dashboard();
    const form = await screen.findByRole('form', { name: 'Quiet hours' });
    const u = userEvent.setup();
    await u.clear(within(form).getByLabelText('Start'));
    await u.type(within(form).getByLabelText('Start'), '25:99');
    await u.click(within(form).getByRole('button', { name: 'Save quiet hours' }));
    const alert = await within(form).findByRole('alert');
    expect(alert.textContent).toContain('The quiet hours were not saved');
    expect(alert.textContent).toContain('The host refuses this value for setting host.quiet_hours.start.');
    expect(alert.textContent).toMatch(/Request id: req-\d+/);
    expect(within(form).getByLabelText('Start').getAttribute('aria-invalid')).toBe('true');
    expect(within(form).getByLabelText('End').getAttribute('aria-invalid')).toBeNull();
    expect(within(form).queryByText('Saved.')).toBeNull();
  });

  it('falls back to a table when the browser cannot draw the chart', async () => {
    const f = fakeHost();
    const s = sample('2026-01-15T10:01:00Z');
    s.total.memory_bytes.value = null;
    f.on('GET', '/admin/v1/host-metrics/history', { body: { data: [sample(), s] } });
    renderApp(f);
    await dashboard();
    const card = await screen.findByRole('region', { name: 'Host use over time' });
    expect(await within(card).findByText(/cannot draw the chart/)).toBeTruthy();
    const table = within(card).getByRole('table');
    const rows = within(table).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(rows[2]!.textContent).toContain('n/a');
    // The history is asked for without a range: the server applies its default.
    const call = f.callsTo('GET', '/admin/v1/host-metrics/history')[0]!;
    expect([...call.query.keys()]).toEqual([]);
  });

  it('says when there are no samples yet', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/host-metrics/history', { body: { data: [] } });
    renderApp(f);
    await dashboard();
    expect(await screen.findByText('No samples yet in this range.')).toBeTruthy();
  });

  it('shows the engine and the tailnet', async () => {
    renderApp(fakeHost());
    await dashboard();
    const engine = await screen.findByRole('region', { name: 'Engine' });
    expect(within(engine).getByText('Answers')).toBeTruthy();
    expect(within(engine).getByText('Version: 0.0.0-engine')).toBeTruthy();
    const tn = screen.getByRole('region', { name: 'Tailnet' });
    expect(within(tn).getByText('stopped')).toBeTruthy();
    expect(within(tn).getByText('Tailscale is switched off.')).toBeTruthy();
    expect(within(tn).getByText('What to do: Switch Tailscale on.')).toBeTruthy();
  });
});
