import { describe, expect, it } from 'vitest';
import { rollup } from '../test/fixtures';
import { granularityFrom, quotaWindow, rowsByBucket, seriesValues, totalOf, totalsByModel, usedOf } from './usage';

describe('quotaWindow', () => {
  it('starts a day window at 00:00 UTC, as the host counts it', () => {
    const now = Date.parse('2026-01-15T10:20:30Z');
    expect(quotaWindow(86400, now)).toEqual({ start: Date.parse('2026-01-15T00:00:00Z'), end: Date.parse('2026-01-16T00:00:00Z') });
    expect(quotaWindow(3600, now).start).toBe(Date.parse('2026-01-15T10:00:00Z'));
  });

  it('counts a week window from 0001-01-01, not from the Unix epoch (Go time.Truncate)', () => {
    // 0001-01-01 was a Monday; Go truncates a 7-day window to Mondays 00:00 UTC.
    const w = quotaWindow(7 * 86400, Date.parse('2026-01-15T10:00:00Z'));
    expect(new Date(w.start).toISOString()).toBe('2026-01-12T00:00:00.000Z');
    expect(new Date(w.start).getUTCDay()).toBe(1);
    expect(w.end - w.start).toBe(7 * 86400 * 1000);
  });

  it('holds now inside the window', () => {
    const now = Date.parse('2026-03-01T23:59:59Z');
    const w = quotaWindow(90, now);
    expect(w.start).toBeLessThanOrEqual(now);
    expect(w.end).toBeGreaterThan(now);
  });
});

describe('granularityFrom', () => {
  it('picks the coarsest bucket that starts exactly at the window start', () => {
    expect(granularityFrom(Date.parse('2026-01-15T00:00:00Z'))).toBe('day');
    expect(granularityFrom(Date.parse('2026-01-15T10:00:00Z'))).toBe('hour');
    expect(granularityFrom(Date.parse('2026-01-15T10:30:00Z'))).toBe('minute');
  });
});

describe('usage totals', () => {
  const rows = [
    rollup(),
    rollup({ access_key_id: 'other-key', requests: 1, total_tokens: 50, prompt_tokens: 30, completion_tokens: 20 }),
    rollup({ model: 'second-model', bucket_start: '2026-01-15T11:00:00Z', requests: 3, total_tokens: 900 }),
  ];

  it('sums keys per model and per bucket', () => {
    expect(totalOf(rows)).toEqual({ requests: 6, prompt_tokens: 230, completion_tokens: 120, total_tokens: 1100 });
    expect(totalsByModel(rows).map((t) => [t.model, t.requests, t.total_tokens])).toEqual([
      ['second-model', 3, 900],
      ['example-model', 3, 200],
    ]);
    const b = rowsByBucket(rows);
    expect(b.map((r) => [r.bucket_start, r.model, r.requests])).toEqual([
      ['2026-01-15T10:00:00Z', 'example-model', 3],
      ['2026-01-15T11:00:00Z', 'second-model', 3],
    ]);
    const xs = [Date.parse('2026-01-15T10:00:00Z') / 1000, Date.parse('2026-01-15T11:00:00Z') / 1000];
    expect(seriesValues(xs, b, 'example-model', (t) => t.requests)).toEqual([3, 0]);
  });

  it('counts tokens or calls for a quota metric', () => {
    const t = totalOf(rows);
    expect(usedOf(t, 'tokens')).toBe(1100);
    expect(usedOf(t, 'requests')).toBe(6);
  });
});
