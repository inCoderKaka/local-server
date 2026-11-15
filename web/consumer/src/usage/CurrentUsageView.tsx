import { formatCount, formatDateTime, formatSeconds } from '@local-server/shared';
import { metricLabel } from '../lib/limits';
import type { CurrentUsage, QuotaUse } from './currentUsage';

const unit = { tokens: 'tokens', requests: 'requests' } as const;

function QuotaLine({ e }: { e: QuotaUse }) {
  const { quota } = e;
  const pct = quota.limit > 0 ? Math.min(100, Math.round((e.used / quota.limit) * 100)) : 100;
  const label = `${metricLabel[quota.metric]} every ${formatSeconds(quota.window_seconds)}`;
  return (
    <li className="flex flex-col gap-1">
      <p>
        <span className="font-medium">{label}:</span> {formatCount(e.used)} of {formatCount(quota.limit)} {unit[quota.metric]} used
      </p>
      <meter className="h-2 w-full" min={0} max={quota.limit > 0 ? quota.limit : 1} value={Math.min(e.used, quota.limit)} aria-label={`${label}: ${pct}% used`} />
      <p className="text-xs text-slate-600">
        This window started {formatDateTime(new Date(e.start))} and the count starts again {formatDateTime(new Date(e.end))}.
      </p>
    </li>
  );
}

/** What is used now, in plain words: per quota window, else the totals over the summary period. */
export function CurrentUsageView({ usage }: { usage: CurrentUsage }) {
  if (usage.kind === 'period') {
    const t = usage.totals;
    return (
      <p className="text-sm">
        {usage.label}: {formatCount(t.requests)} {t.requests === 1 ? 'call' : 'calls'}, {formatCount(t.total_tokens)} tokens. Your grant has no quota,
        so nothing is counted against a limit.
      </p>
    );
  }
  return (
    <ul className="flex flex-col gap-3 text-sm" aria-label="Use of each quota">
      {usage.entries.map((e) => (
        <QuotaLine key={`${e.quota.metric}-${e.quota.window_seconds}`} e={e} />
      ))}
    </ul>
  );
}

/** The note under every view of usage: usage shows after the host's next rollup pass. */
export function RollupNote() {
  return (
    <p className="text-xs text-slate-600">
      A call shows here after the host's next usage rollup, so the newest calls can take a short while to appear. Buckets are counted in UTC; times are
      shown in your browser's time zone.
    </p>
  );
}
