import { useId, useMemo, useState } from 'react';
import {
  bucketStarts,
  Button,
  canDraw,
  Card,
  DEFAULT_GRANULARITY,
  ErrorBanner,
  formatCount,
  formatDateTime,
  granularityLabel,
  Plot,
  RANGES,
  Spinner,
  Table,
  useApi,
  useClient,
  UserText,
  type Granularity,
  type Series,
} from '@local-server/shared';
import { SERIES_COLOURS } from '../lib/constants';
import { useMyLimits } from '../lib/limits';
import { readUsage, rowsByBucket, seriesValues, totalsByModel, type BucketRow, type ModelTotals, type Totals } from '../lib/usage';
import { useCurrentUsage } from './currentUsage';
import { CurrentUsageView, RollupNote } from './CurrentUsageView';

const selectClass = 'rounded border border-slate-300 px-2 py-1';

/**
 * My usage: what is used now of each quota window, and the usage over a chosen range per model as
 * a chart and the same data as a table (GET /v1/usage). Only the caller's own consumer is ever
 * shown: the host scopes the route to the session.
 */
export default function UsagePage() {
  const client = useClient();
  const ids = { granularity: useId(), range: useId() };
  const [granularity, setGranularity] = useState<Granularity>(DEFAULT_GRANULARITY);
  const [rangeId, setRangeId] = useState(RANGES[DEFAULT_GRANULARITY][0]!.id);
  const [now, setNow] = useState(() => Date.now());
  const range = RANGES[granularity].find((r) => r.id === rangeId) ?? RANGES[granularity][0]!;
  const from = now - range.ms;

  const limits = useMyLimits();
  const current = useCurrentUsage(limits.data === undefined ? undefined : (limits.data?.limits ?? null), now);
  const usage = useApi(() => readUsage(client, granularity, from, now), [client, granularity, from, now]);
  const [drawable] = useState(canDraw);

  const rows = usage.data;
  const totals = useMemo(() => (rows ? totalsByModel(rows) : []), [rows]);
  const buckets = useMemo(() => (rows ? rowsByBucket(rows) : []), [rows]);
  // Stable inputs for the plots, so they redraw only when the data changes.
  const chart = useMemo<{ xs: number[]; calls: Series[]; tokens: Series[] } | null>(() => {
    if (!rows) return null;
    const xs = bucketStarts(granularity, from, now);
    const series = (pick: (t: Totals) => number) =>
      totals.map((t, i) => ({ label: t.model, values: seriesValues(xs, buckets, t.model, pick), stroke: SERIES_COLOURS[i % SERIES_COLOURS.length]! }));
    return { xs, calls: series((t) => t.requests), tokens: series((t) => t.total_tokens) };
  }, [rows, totals, buckets, granularity, from, now]);

  function refresh() {
    setNow(Date.now());
    void limits.refetch();
  }

  function chooseGranularity(g: Granularity) {
    setGranularity(g);
    setRangeId(RANGES[g][0]!.id);
    setNow(Date.now());
  }

  return (
    <>
      <h1 className="text-xl font-semibold">My usage</h1>
      <Card title="Used now" actions={<Button onClick={refresh}>Refresh</Button>}>
        <div className="flex flex-col gap-2">
          <ErrorBanner error={limits.error ?? current.error} title="Your current usage could not be read" />
          {current.loading && !current.data && <Spinner label="Loading your current usage" />}
          {current.data && <CurrentUsageView usage={current.data} />}
          <RollupNote />
        </div>
      </Card>

      <Card title="What to show">
        <div className="flex flex-wrap items-end gap-4 text-sm">
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.granularity} className="font-medium">
              Bucket size
            </label>
            <select id={ids.granularity} value={granularity} onChange={(e) => chooseGranularity(e.target.value as Granularity)} className={selectClass}>
              {(Object.keys(granularityLabel) as Granularity[]).map((g) => (
                <option key={g} value={g}>
                  {granularityLabel[g]}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.range} className="font-medium">
              Range
            </label>
            <select
              id={ids.range}
              value={range.id}
              onChange={(e) => {
                setRangeId(e.target.value);
                setNow(Date.now());
              }}
              className={selectClass}
            >
              {RANGES[granularity].map((r) => (
                <option key={r.id} value={r.id}>
                  {r.label}
                </option>
              ))}
            </select>
          </div>
        </div>
        <p className="mt-3 text-xs text-slate-600">
          The host keeps finer buckets for a shorter time than coarser ones. An older range can show no usage per minute or per hour; choose a larger
          bucket size then.
        </p>
      </Card>

      <Card title="Totals per model">
        <ErrorBanner error={usage.error} title="Your usage could not be read" />
        {usage.loading && !rows && <Spinner label="Loading your usage" />}
        {rows && (
          <Table<ModelTotals>
            caption={`Usage from ${formatDateTime(new Date(from))} to ${formatDateTime(new Date(now))}`}
            rowKey={(t) => t.model}
            rows={totals}
            empty="No calls in this range."
            columns={[
              { key: 'm', header: 'Model', render: (t) => <UserText text={t.model} /> },
              { key: 'r', header: 'Calls', render: (t) => formatCount(t.requests) },
              { key: 'p', header: 'Prompt tokens', render: (t) => formatCount(t.prompt_tokens) },
              { key: 'g', header: 'Generated tokens', render: (t) => formatCount(t.completion_tokens) },
              { key: 't', header: 'Total tokens', render: (t) => formatCount(t.total_tokens) },
            ]}
          />
        )}
      </Card>

      {rows && rows.length > 0 && (
        <Card title="Usage over time">
          {chart && drawable && (
            <div className="flex flex-col gap-4">
              <Plot title="Calls" xs={chart.xs} series={chart.calls} format={formatCount} />
              <Plot title="Tokens" xs={chart.xs} series={chart.tokens} format={formatCount} />
            </div>
          )}
          {!drawable && <p className="text-sm text-slate-600">This browser cannot draw the chart; the table below has the same data.</p>}
          <details className="mt-3" open={!drawable}>
            <summary className="cursor-pointer text-sm">Show the data as a table</summary>
            <Table<BucketRow>
              caption={`Usage ${granularityLabel[granularity].toLowerCase()}, oldest first; buckets without calls are left out`}
              rowKey={(r) => `${r.bucket_start}|${r.model}`}
              rows={buckets}
              columns={[
                { key: 'b', header: 'Bucket start', render: (r) => formatDateTime(r.bucket_start) },
                { key: 'm', header: 'Model', render: (r) => <UserText text={r.model} /> },
                { key: 'r', header: 'Calls', render: (r) => formatCount(r.requests) },
                { key: 't', header: 'Total tokens', render: (r) => formatCount(r.total_tokens) },
              ]}
            />
          </details>
        </Card>
      )}
    </>
  );
}
