import type { Schema } from './api/client';

/** A usage bucket size (GET /v1/usage and /admin/v1/usage `granularity`). */
export type Granularity = Schema<'UsageGranularity'>;

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** The length of one bucket. Buckets are UTC-aligned, so a day bucket is always 24 hours. */
export const bucketMs: Record<Granularity, number> = { minute: MINUTE, hour: HOUR, day: DAY };

export const granularityLabel: Record<Granularity, string> = { minute: 'Per minute', hour: 'Per hour', day: 'Per day' };

export interface RangeChoice {
  id: string;
  label: string;
  ms: number;
}

/**
 * The ranges offered for each bucket size, and the first is the default. These are display
 * choices of the usage pages, not host settings: how far back each bucket size is kept is the host's
 * usage.rollup.*_retention setting, and the page says so (req_001 D-38). Each list stays short
 * enough that the chart has at most a few thousand points.
 */
export const RANGES: Record<Granularity, RangeChoice[]> = {
  minute: [
    { id: '1h', label: 'Last hour', ms: HOUR },
    { id: '6h', label: 'Last 6 hours', ms: 6 * HOUR },
    { id: '24h', label: 'Last 24 hours', ms: DAY },
  ],
  hour: [
    { id: '24h', label: 'Last 24 hours', ms: DAY },
    { id: '7d', label: 'Last 7 days', ms: 7 * DAY },
    { id: '30d', label: 'Last 30 days', ms: 30 * DAY },
    { id: '90d', label: 'Last 90 days', ms: 90 * DAY },
  ],
  day: [
    { id: '30d', label: 'Last 30 days', ms: 30 * DAY },
    { id: '90d', label: 'Last 90 days', ms: 90 * DAY },
    { id: '365d', label: 'Last 365 days', ms: 365 * DAY },
  ],
};

/** What a usage page shows first: hour buckets of the last 24 hours. */
export const DEFAULT_GRANULARITY: Granularity = 'hour';

/**
 * Every bucket start in [from, to), in seconds for the chart. The API returns only buckets with
 * calls, so a bucket without a row had no calls and counts as zero.
 */
export function bucketStarts(granularity: Granularity, from: number, to: number): number[] {
  const step = bucketMs[granularity];
  const out: number[] = [];
  for (let t = Math.ceil(from / step) * step; t < to; t += step) out.push(t / 1000);
  return out;
}
