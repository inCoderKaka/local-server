import type { Schema } from '@local-server/shared';

export type Rollup = Schema<'UsageRollup'>;
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
 * choices of this page, not host settings: how far back each bucket size is kept is the host's
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

/** What the page shows first: hour buckets of the last 24 hours. */
export const DEFAULT_GRANULARITY: Granularity = 'hour';

export interface Totals {
  consumer_id: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

function add(t: Totals, r: Rollup) {
  t.requests += r.requests;
  t.prompt_tokens += r.prompt_tokens;
  t.completion_tokens += r.completion_tokens;
  t.total_tokens += r.total_tokens;
}

function empty(consumer_id: string): Totals {
  return { consumer_id, requests: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 };
}

/** The totals per consumer over every rollup (all keys and models), most tokens first. */
export function totalsByConsumer(rows: Rollup[]): Totals[] {
  const m = new Map<string, Totals>();
  for (const r of rows) {
    let t = m.get(r.consumer_id);
    if (!t) m.set(r.consumer_id, (t = empty(r.consumer_id)));
    add(t, r);
  }
  return [...m.values()].sort((a, b) => b.total_tokens - a.total_tokens || a.consumer_id.localeCompare(b.consumer_id));
}

export interface BucketRow extends Totals {
  bucket_start: string;
}

/** The totals per bucket and consumer (keys and models summed), oldest bucket first. */
export function rowsByBucket(rows: Rollup[]): BucketRow[] {
  const m = new Map<string, BucketRow>();
  for (const r of rows) {
    const k = `${r.bucket_start}|${r.consumer_id}`;
    let t = m.get(k);
    if (!t) m.set(k, (t = { ...empty(r.consumer_id), bucket_start: r.bucket_start }));
    add(t, r);
  }
  return [...m.values()].sort((a, b) => Date.parse(a.bucket_start) - Date.parse(b.bucket_start) || a.consumer_id.localeCompare(b.consumer_id));
}

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

/** One value per bucket start and consumer, zero where there is no row. */
export function seriesValues(xs: number[], rows: BucketRow[], consumerId: string, pick: (t: Totals) => number): number[] {
  const at = new Map<number, number>();
  for (const r of rows) if (r.consumer_id === consumerId) at.set(Date.parse(r.bucket_start) / 1000, pick(r));
  return xs.map((x) => at.get(x) ?? 0);
}
