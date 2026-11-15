import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { bucketStarts, type Schema } from '@local-server/shared';
import { consumer } from '../consumers/testData';
import { fakeHost, settingsList } from '../test/fixtures';
import { renderApp } from '../test/render';
import { seriesValues, rowsByBucket } from './usageData';

const A = '0192e5a0-0001-7000-8000-000000000001';
const B = '0192e5a0-0001-7000-8000-0000000000b2';
const XSS = '<img src=x onerror=alert(1)>';

function rollup(over: Partial<Schema<'UsageRollup'>> = {}): Schema<'UsageRollup'> {
  return {
    bucket_start: '2026-01-15T10:00:00Z',
    granularity: 'hour',
    consumer_id: A,
    access_key_id: '0192e5a0-0002-7000-8000-000000000002',
    model: 'example-model',
    requests: 2,
    prompt_tokens: 100,
    completion_tokens: 50,
    total_tokens: 150,
    ...over,
  };
}

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/settings', {
    body: {
      data: [
        ...settingsList(),
        { key: 'usage.rollup.hour_retention', owner: 'host', value: '2160h', default_value: '2160h' },
        { key: 'usage.rollup.day_retention', owner: 'host', value: '0s', default_value: '0s' },
        { key: 'usage.rollup.interval', owner: 'host', value: '1m', default_value: '1m' },
      ],
    },
  });
  f.on('GET', '/admin/v1/consumers', { body: { data: [consumer({ id: A, display_name: 'Example One' }), consumer({ id: B, display_name: XSS })], next_cursor: null } });
  // Two pages: the page follows the cursor and adds them up.
  f.on('GET', '/admin/v1/usage', (c) =>
    c.query.get('cursor') === 'p2'
      ? { body: { data: [rollup({ bucket_start: '2026-01-15T11:00:00Z', consumer_id: B, requests: 1, total_tokens: 10, prompt_tokens: 6, completion_tokens: 4 })], next_cursor: null } }
      : { body: { data: [rollup(), rollup({ model: 'other-model', requests: 3, total_tokens: 30, prompt_tokens: 20, completion_tokens: 10 })], next_cursor: 'p2' } },
  );
  return f;
}

async function totals() {
  return screen.findByRole('table', { name: /^Usage from/ });
}

describe('usage page', () => {
  it('reads every page and shows the totals per consumer, names as text', async () => {
    const f = host();
    renderApp(f, { hash: '#/usage' });
    const t = await totals();
    const rows = within(t).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(rows[1]!.textContent).toContain('Example One');
    expect(rows[1]!.textContent).toContain('5'); // 2 + 3 calls
    expect(rows[1]!.textContent).toContain('180');
    expect(rows[2]!.textContent).toContain(XSS);
    expect(document.querySelector('img')).toBeNull();
    const calls = f.callsTo('GET', '/admin/v1/usage');
    expect(calls).toHaveLength(2);
    expect(calls[0]!.query.get('granularity')).toBe('hour');
    expect(calls[0]!.query.get('consumer_id')).toBeNull();
    const from = Date.parse(calls[0]!.query.get('from')!);
    const to = Date.parse(calls[0]!.query.get('to')!);
    expect(to - from).toBe(24 * 3600_000);
    expect(calls[1]!.query.get('cursor')).toBe('p2');
    // The host's own settings explain retention and lag.
    expect(screen.getByText(/keeps hour totals for 2160h/)).toBeTruthy();
    expect(screen.getByText(/runs every 1m/)).toBeTruthy();
  });

  it('shows the data over time as a table where no chart can be drawn', async () => {
    renderApp(host(), { hash: '#/usage' });
    await totals();
    expect(screen.getByText(/cannot draw the chart/)).toBeTruthy();
    const t = screen.getByRole('table', { name: /oldest first/ });
    const rows = within(t).getAllByRole('row');
    // Two models of A in one bucket are summed; B is in the next bucket.
    expect(rows).toHaveLength(3);
    expect(rows[1]!.textContent).toContain('Example One');
    expect(rows[1]!.textContent).toContain('180');
    expect(rows[2]!.textContent).toContain('10');
  });

  it('picks a consumer from the query and from the picker, and a bucket size', async () => {
    const f = host();
    renderApp(f, { hash: `#/usage?consumer=${B}` });
    await totals();
    expect(f.callsTo('GET', '/admin/v1/usage')[0]!.query.get('consumer_id')).toBe(B);
    const picker = (await screen.findByLabelText('Consumer')) as HTMLSelectElement;
    await waitFor(() => expect(picker.value).toBe(B));
    fireEvent.change(picker, { target: { value: A } });
    await waitFor(() => expect(f.callsTo('GET', '/admin/v1/usage').some((c) => c.query.get('consumer_id') === A)).toBe(true));
    fireEvent.change(screen.getByLabelText('Bucket size'), { target: { value: 'day' } });
    await waitFor(() => expect(f.callsTo('GET', '/admin/v1/usage').some((c) => c.query.get('granularity') === 'day')).toBe(true));
    const last = f.callsTo('GET', '/admin/v1/usage').filter((c) => c.query.get('granularity') === 'day')[0]!;
    expect(Date.parse(last.query.get('to')!) - Date.parse(last.query.get('from')!)).toBe(30 * 24 * 3600_000);
    expect(screen.getByText(/keeps day totals forever/)).toBeTruthy();
  });

  it('says so when there are no calls in the range', async () => {
    const f = host();
    f.on('GET', '/admin/v1/usage', { body: { data: [], next_cursor: null } });
    renderApp(f, { hash: '#/usage' });
    await totals();
    expect(screen.getByText('No calls in this range.')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Usage over time' })).toBeNull();
  });

  it('shows the API error', async () => {
    const f = host();
    f.on('GET', '/admin/v1/usage', errorReply(400, 'invalid_request', 'The range is not valid.'));
    renderApp(f, { hash: '#/usage' });
    expect(await screen.findByText('The range is not valid.')).toBeTruthy();
    expect(screen.getByText('The usage could not be read')).toBeTruthy();
  });

  it('fills buckets without a row with zero for the chart', () => {
    const from = Date.parse('2026-01-15T09:30:00Z');
    const to = Date.parse('2026-01-15T12:00:00Z');
    const xs = bucketStarts('hour', from, to);
    expect(xs.map((x) => new Date(x * 1000).toISOString())).toEqual(['2026-01-15T10:00:00.000Z', '2026-01-15T11:00:00.000Z']);
    const rows = rowsByBucket([rollup(), rollup({ total_tokens: 5 })]);
    expect(seriesValues(xs, rows, A, (t) => t.total_tokens)).toEqual([155, 0]);
  });
});
