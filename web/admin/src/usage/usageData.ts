import type { Schema } from '@local-server/shared';

export type Rollup = Schema<'UsageRollup'>;

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

/** One value per bucket start and consumer, zero where there is no row. */
export function seriesValues(xs: number[], rows: BucketRow[], consumerId: string, pick: (t: Totals) => number): number[] {
  const at = new Map<number, number>();
  for (const r of rows) if (r.consumer_id === consumerId) at.set(Date.parse(r.bucket_start) / 1000, pick(r));
  return xs.map((x) => at.get(x) ?? 0);
}
