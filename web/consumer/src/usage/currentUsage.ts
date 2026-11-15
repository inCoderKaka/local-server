import { useMemo } from 'react';
import { useApi, useClient, type ApiState } from '@local-server/shared';
import { SUMMARY_PERIOD_WITHOUT_QUOTA } from '../lib/constants';
import type { GrantLimits, QuotaEntry } from '../lib/limits';
import { granularityFrom, quotaWindow, readUsage, totalOf, usedOf, type Totals } from '../lib/usage';

/** One quota entry with what is used of it in its current window. */
export interface QuotaUse {
  quota: QuotaEntry;
  /** The current window, in ms. */
  start: number;
  end: number;
  used: number;
}

/** What is used now: per quota window, or, for a grant without quotas, over the summary period. */
export type CurrentUsage = { kind: 'quotas'; entries: QuotaUse[] } | { kind: 'period'; label: string; from: number; totals: Totals };

/**
 * The usage of the current quota windows of the grant, summed from the usage rollups (GET
 * /v1/usage) from each window's start, in buckets that start exactly there. A grant without
 * quotas, or no grant, gives the totals of the summary period instead. Usage shows after the
 * host's next rollup pass, so this can trail the host's own quota count by that much.
 */
export function useCurrentUsage(limits: GrantLimits | null | undefined, now: number): ApiState<CurrentUsage> {
  const client = useClient();
  const quotas = useMemo(() => limits?.quotas ?? [], [limits]);
  const key = quotas.map((q) => `${q.metric}:${q.limit}:${q.window_seconds}`).join(',');
  return useApi<CurrentUsage>(
    async () => {
      if (quotas.length === 0) {
        const p = SUMMARY_PERIOD_WITHOUT_QUOTA;
        const from = now - p.range.ms;
        const rows = await readUsage(client, p.granularity, from, now);
        return { kind: 'period', label: p.range.label, from, totals: totalOf(rows) };
      }
      // One read per window length: two entries with the same window share it.
      const byWindow = new Map<string, Promise<Totals>>();
      const entries = await Promise.all(
        quotas.map(async (quota) => {
          const { start, end } = quotaWindow(quota.window_seconds, now);
          let t = byWindow.get(`${start}-${end}`);
          if (!t) {
            t = readUsage(client, granularityFrom(start), start, end).then(totalOf);
            byWindow.set(`${start}-${end}`, t);
          }
          return { quota, start, end, used: usedOf(await t, quota.metric) };
        }),
      );
      return { kind: 'quotas', entries };
    },
    [client, key, now],
    { enabled: limits !== undefined },
  );
}
