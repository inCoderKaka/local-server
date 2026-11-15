import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { formatCount } from '@local-server/shared';
import { errorReply } from '@local-server/shared/test';
import { grantVersion, noGrant, rollup, signedIn } from '../test/fixtures';
import { renderApp } from '../test/render';

const HOUR = 3_600_000;
const XSS = '<img src=x onerror=alert(1)>';

/** The start of this hour and of the hour before, as the host writes bucket starts. */
function hours() {
  const h = Math.floor(Date.now() / HOUR) * HOUR;
  return { now: new Date(h).toISOString(), before: new Date(h - HOUR).toISOString() };
}

/** A host with usage: day buckets for the quota window, hour buckets for the range. */
function host() {
  const f = signedIn();
  const { now, before } = hours();
  f.on('GET', '/v1/usage', (c) => {
    const g = c.query.get('granularity');
    if (g === 'day') return { body: { data: [rollup({ granularity: 'day', total_tokens: 1234, requests: 7 })], next_cursor: null } };
    return {
      body: {
        data: [
          rollup({ bucket_start: before, requests: 2, total_tokens: 150 }),
          rollup({ bucket_start: now, requests: 1, total_tokens: 40, prompt_tokens: 30, completion_tokens: 10 }),
          rollup({ bucket_start: now, model: XSS, requests: 4, total_tokens: 400 }),
        ],
        next_cursor: null,
      },
    };
  });
  return f;
}

describe('my usage', () => {
  it('shows what is used now of the quota window and the totals per model', async () => {
    const f = host();
    const view = renderApp(f, { hash: '#/usage' });
    expect(await screen.findByRole('heading', { level: 1, name: 'My usage' })).toBeTruthy();
    const nowCard = screen.getByRole('region', { name: 'Used now' });
    await waitFor(() => expect(nowCard.textContent).toContain(`${formatCount(1234)} of ${formatCount(100000)} tokens used`));
    expect(within(nowCard).getByRole('meter', { name: /Tokens every 24h: 1% used/ })).toBeTruthy();
    // The day window is read in day buckets from 00:00 UTC.
    const day = f.calls.find((c) => c.path === '/v1/usage' && c.query.get('granularity') === 'day')!;
    expect(new Date(day.query.get('from')!).toISOString()).toMatch(/T00:00:00\.000Z$/);

    const totals = screen.getByRole('region', { name: 'Totals per model' });
    const rows = await within(totals).findAllByRole('row');
    expect(within(rows[1]!).getAllByRole('cell').map((c) => c.textContent)).toEqual([XSS, '4', '100', '50', '400']);
    expect(within(rows[1]!).getByText(XSS)).toBeTruthy();
    expect(view.container.querySelector('img')).toBeNull();
    const example = rows.find((r) => r.textContent!.startsWith('example-model'))!;
    expect(within(example).getAllByRole('cell').map((c) => c.textContent)).toEqual(['example-model', '3', '130', '60', '190']);

    const overTime = screen.getByRole('region', { name: 'Usage over time' });
    // jsdom has no canvas: the table is open and says why.
    expect(within(overTime).getByText(/cannot draw the chart/)).toBeTruthy();
    const table = within(overTime).getByRole('table', { name: /Usage per hour, oldest first/ });
    expect(within(table).getAllByRole('row')).toHaveLength(4);
    // Hour buckets of the last 24 hours, the first choice.
    const hour = f.calls.find((c) => c.path === '/v1/usage' && c.query.get('granularity') === 'hour')!;
    expect(Date.parse(hour.query.get('to')!) - Date.parse(hour.query.get('from')!)).toBe(24 * HOUR);
  });

  it('reads the chosen bucket size and range again', async () => {
    const f = host();
    renderApp(f, { hash: '#/usage' });
    await screen.findByRole('region', { name: 'Usage over time' });
    const u = userEvent.setup();
    await u.selectOptions(screen.getByLabelText('Bucket size'), 'day');
    await waitFor(() => expect(f.calls.some((c) => c.query.get('granularity') === 'day' && Date.parse(c.query.get('to')!) - Date.parse(c.query.get('from')!) === 30 * 24 * HOUR)).toBe(true));
    expect((screen.getByLabelText('Range') as HTMLSelectElement).value).toBe('30d');
  });

  it('says when there are no calls, and sums the summary period for a grant without quota', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', { body: grantVersion({ quotas: [] }) });
    renderApp(f, { hash: '#/usage' });
    expect(await screen.findByText('No calls in this range.')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Usage over time' })).toBeNull();
    const nowCard = screen.getByRole('region', { name: 'Used now' });
    await waitFor(() => expect(nowCard.textContent).toContain('Last 24 hours: 0 calls, 0 tokens. Your grant has no quota'));
  });

  it('works without a grant', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', noGrant);
    renderApp(f, { hash: '#/usage' });
    const nowCard = await screen.findByRole('region', { name: 'Used now' });
    await waitFor(() => expect(nowCard.textContent).toContain('Last 24 hours: 0 calls'));
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it("shows the host's error", async () => {
    const f = signedIn();
    f.on('GET', '/v1/usage', errorReply(400, 'invalid_request', 'The range is too long.'));
    renderApp(f, { hash: '#/usage' });
    await waitFor(() => expect(screen.getAllByRole('alert').map((a) => a.textContent).join(' ')).toContain('Your usage could not be read'));
    expect(screen.getAllByRole('alert')[0]!.textContent).toContain('The range is too long.');
  });
});
