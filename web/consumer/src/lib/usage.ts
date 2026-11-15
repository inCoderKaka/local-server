import { bucketMs, call, readAll, type ApiClient, type Granularity, type Schema } from '@local-server/shared';

export type Rollup = Schema<'UsageRollup'>;
export type QuotaEntry = Schema<'QuotaEntry'>;

/**
 * The caller's usage rollups in [from, to), every page (GET /v1/usage); the host pages them with
 * its own page size. Buckets are UTC-aligned and only buckets with calls have rows.
 */
export function readUsage(client: ApiClient, granularity: Granularity, from: number, to: number): Promise<Rollup[]> {
  return readAll<Rollup>((cursor) =>
    call(client.GET('/v1/usage', { params: { query: { granularity, from: new Date(from).toISOString(), to: new Date(to).toISOString(), cursor } } })),
  );
}

export interface Totals {
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ModelTotals extends Totals {
  model: string;
}

export interface BucketRow extends ModelTotals {
  bucket_start: string;
}

function zero(): Totals {
  return { requests: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 };
}

function add(t: Totals, r: Totals) {
  t.requests += r.requests;
  t.prompt_tokens += r.prompt_tokens;
  t.completion_tokens += r.completion_tokens;
  t.total_tokens += r.total_tokens;
}

/** The totals over every row (all keys and models). */
export function totalOf(rows: Rollup[]): Totals {
  const t = zero();
  for (const r of rows) add(t, r);
  return t;
}

/** The totals per model (keys summed), most tokens first. */
export function totalsByModel(rows: Rollup[]): ModelTotals[] {
  const m = new Map<string, ModelTotals>();
  for (const r of rows) {
    let t = m.get(r.model);
    if (!t) m.set(r.model, (t = { model: r.model, ...zero() }));
    add(t, r);
  }
  return [...m.values()].sort((a, b) => b.total_tokens - a.total_tokens || a.model.localeCompare(b.model));
}

/** The totals per bucket and model (keys summed), oldest bucket first. */
export function rowsByBucket(rows: Rollup[]): BucketRow[] {
  const m = new Map<string, BucketRow>();
  for (const r of rows) {
    const k = `${r.bucket_start}|${r.model}`;
    let t = m.get(k);
    if (!t) m.set(k, (t = { bucket_start: r.bucket_start, model: r.model, ...zero() }));
    add(t, r);
  }
  return [...m.values()].sort((a, b) => Date.parse(a.bucket_start) - Date.parse(b.bucket_start) || a.model.localeCompare(b.model));
}

/** One value per bucket start (seconds) for one model, zero where the model has no row. */
export function seriesValues(xs: number[], rows: BucketRow[], model: string, pick: (t: Totals) => number): number[] {
  const at = new Map<number, number>();
  for (const r of rows) if (r.model === model) at.set(Date.parse(r.bucket_start) / 1000, pick(r));
  return xs.map((x) => at.get(x) ?? 0);
}

/** What a quota entry counts, out of a total: tokens, or calls for `requests`. */
export function usedOf(t: Totals, metric: QuotaEntry['metric']): number {
  return metric === 'tokens' ? t.total_tokens : t.requests;
}

/** Seconds from Go's zero time (0001-01-01 00:00 UTC) to the Unix epoch. */
const GO_ZERO_TO_UNIX_SECONDS = 62_135_596_800;

/**
 * The fixed quota window of `windowSeconds` that holds `now` (ms), as the host counts it: windows
 * are whole multiples of their length counted from 0001-01-01 00:00 UTC (internal/limits, Go's
 * time.Truncate), so a length that divides a day starts at 00:00 UTC.
 */
export function quotaWindow(windowSeconds: number, now: number): { start: number; end: number } {
  const w = Math.max(1, Math.trunc(windowSeconds));
  const s = Math.floor(now / 1000);
  const start = (s - ((s + GO_ZERO_TO_UNIX_SECONDS) % w)) * 1000;
  return { start, end: start + w * 1000 };
}

/**
 * The coarsest bucket size whose buckets start exactly at `start`, so summing the buckets from
 * there counts the whole window and nothing before it.
 */
export function granularityFrom(start: number): Granularity {
  if (start % bucketMs.day === 0) return 'day';
  if (start % bucketMs.hour === 0) return 'hour';
  return 'minute';
}
