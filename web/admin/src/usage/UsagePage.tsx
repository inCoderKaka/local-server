import { useId, useMemo, useState } from 'react';
import {
  bucketStarts,
  Button,
  call,
  canDraw,
  Card,
  DEFAULT_GRANULARITY,
  ErrorBanner,
  formatCount,
  formatDateTime,
  granularityLabel,
  Link,
  parseGoDuration,
  Plot,
  RANGES,
  readAll,
  Spinner,
  Table,
  useApi,
  useClient,
  useHashLocation,
  type ApiClient,
  type Granularity,
  type Series,
} from '@local-server/shared';
import { ConsumerName, type Consumer } from '../consumers/labels';
import { settingOf, useSettings } from '../lib/settings';
import { rowsByBucket, seriesValues, totalsByConsumer, type BucketRow, type Rollup, type Totals } from './usageData';

/** The host settings this page reads to explain what it shows (owned by the usage module). */
const RETENTION_KEY: Record<Granularity, string> = {
  minute: 'usage.rollup.minute_retention',
  hour: 'usage.rollup.hour_retention',
  day: 'usage.rollup.day_retention',
};
const ROLLUP_INTERVAL_KEY = 'usage.rollup.interval';

const palette = ['#0284c7', '#d97706', '#059669', '#7c3aed', '#dc2626', '#334155', '#db2777', '#65a30d'];

function readUsage(client: ApiClient, granularity: Granularity, from: number, to: number, consumerId: string) {
  return readAll<Rollup>((cursor) =>
    call(
      client.GET('/admin/v1/usage', {
        params: {
          query: {
            granularity,
            from: new Date(from).toISOString(),
            to: new Date(to).toISOString(),
            consumer_id: consumerId || undefined,
            cursor,
          },
        },
      }),
    ),
  );
}

/** How long the host keeps a bucket size, from its own setting: "forever" at zero or less. */
function retentionText(value: unknown): string | null {
  const ms = parseGoDuration(value);
  if (ms === null) return null;
  return ms <= 0 ? 'forever' : String(value);
}

function Notes({ granularity }: { granularity: Granularity }) {
  const { list } = useSettings();
  const kept = retentionText(settingOf(list, RETENTION_KEY[granularity])?.value);
  const interval = settingOf(list, ROLLUP_INTERVAL_KEY)?.value;
  return (
    <ul className="list-disc pl-5 text-sm text-slate-600">
      <li>Buckets start on UTC minutes, hours and days; times are shown in your browser's time zone.</li>
      {kept && (
        <li>
          This host keeps {granularity} totals {kept === 'forever' ? 'forever' : `for ${kept}`} (setting <code>{RETENTION_KEY[granularity]}</code>). An
          older range shows no usage at this bucket size, so choose a larger one.
        </li>
      )}
      {typeof interval === 'string' && (
        <li>
          New calls show after the next rollup pass, which runs every {interval} (setting <code>{ROLLUP_INTERVAL_KEY}</code>).
        </li>
      )}
    </ul>
  );
}

function NameOf({ id, consumers }: { id: string; consumers: Map<string, Consumer> }) {
  const c = consumers.get(id);
  return (
    <Link to={`/consumers/${encodeURIComponent(id)}`} className="text-sky-800 underline">
      {c ? <ConsumerName consumer={c} /> : <span className="font-mono text-xs">{id}</span>}
    </Link>
  );
}

/** Usage per consumer over a range: totals, a chart over time and the same data as a table (D-29). */
export default function UsagePage() {
  const client = useClient();
  const { query } = useHashLocation();
  const ids = { consumer: useId(), granularity: useId(), range: useId() };
  const [consumerId, setConsumerId] = useState(() => query.get('consumer') ?? '');
  const [granularity, setGranularity] = useState<Granularity>(DEFAULT_GRANULARITY);
  const [rangeId, setRangeId] = useState(RANGES[DEFAULT_GRANULARITY][0]!.id);
  const [now, setNow] = useState(() => Date.now());
  const range = RANGES[granularity].find((r) => r.id === rangeId) ?? RANGES[granularity][0]!;
  const from = now - range.ms;

  const consumers = useApi(
    () => readAll<Consumer>((cursor) => call(client.GET('/admin/v1/consumers', { params: { query: { cursor } } }))),
    [client],
  );
  const usage = useApi(() => readUsage(client, granularity, from, now, consumerId), [client, granularity, from, now, consumerId]);
  const byId = useMemo(() => new Map((consumers.data ?? []).map((c) => [c.id, c])), [consumers.data]);
  const [drawable] = useState(canDraw);

  const rows = usage.data;
  const totals = useMemo(() => (rows ? totalsByConsumer(rows) : []), [rows]);
  const buckets = useMemo(() => (rows ? rowsByBucket(rows) : []), [rows]);
  // Stable inputs for the plots, so they redraw only when the data changes.
  const chart = useMemo<{ xs: number[]; calls: Series[]; tokens: Series[] } | null>(() => {
    if (!rows) return null;
    const xs = bucketStarts(granularity, from, now);
    // uPlot writes a series label with textContent, never as HTML (D-31).
    const series = (pick: (t: Totals) => number) =>
      totals.map((t, i) => ({
        label: byId.get(t.consumer_id)?.display_name ?? t.consumer_id,
        values: seriesValues(xs, buckets, t.consumer_id, pick),
        stroke: palette[i % palette.length]!,
      }));
    return { xs, calls: series((t) => t.requests), tokens: series((t) => t.total_tokens) };
  }, [rows, totals, buckets, byId, granularity, from, now]);

  function chooseGranularity(g: Granularity) {
    setGranularity(g);
    setRangeId(RANGES[g][0]!.id);
    setNow(Date.now());
  }

  const selectClass = 'rounded border border-slate-300 px-2 py-1';
  const known = consumerId === '' || byId.has(consumerId);
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Usage</h1>
      <Card title="What to show" actions={<Button onClick={() => setNow(Date.now())}>Refresh</Button>}>
        <div className="flex flex-wrap items-end gap-4 text-sm">
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.consumer} className="font-medium">
              Consumer
            </label>
            <select id={ids.consumer} value={consumerId} onChange={(e) => setConsumerId(e.target.value)} className={selectClass}>
              <option value="">All consumers</option>
              {/* Option text is a plain text node, never HTML (D-31). */}
              {(consumers.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.display_name ?? c.id}
                </option>
              ))}
              {!known && <option value={consumerId}>{consumerId}</option>}
            </select>
          </div>
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
        <div className="mt-3">
          <Notes granularity={granularity} />
        </div>
        <ErrorBanner error={consumers.error} title="The consumers could not be read" />
      </Card>

      <Card title="Totals per consumer">
        <ErrorBanner error={usage.error} title="The usage could not be read" />
        {usage.loading && <Spinner label="Loading the usage" />}
        {rows && (
          <Table<Totals>
            caption={`Usage from ${formatDateTime(new Date(from))} to ${formatDateTime(new Date(now))}`}
            rowKey={(t) => t.consumer_id}
            rows={totals}
            empty="No calls in this range."
            columns={[
              { key: 'c', header: 'Consumer', render: (t) => <NameOf id={t.consumer_id} consumers={byId} /> },
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
              rowKey={(r) => `${r.bucket_start}|${r.consumer_id}`}
              rows={buckets}
              columns={[
                { key: 'b', header: 'Bucket start', render: (r) => formatDateTime(r.bucket_start) },
                { key: 'c', header: 'Consumer', render: (r) => <NameOf id={r.consumer_id} consumers={byId} /> },
                { key: 'r', header: 'Calls', render: (r) => formatCount(r.requests) },
                { key: 't', header: 'Total tokens', render: (r) => formatCount(r.total_tokens) },
              ]}
            />
          </details>
        </Card>
      )}
    </div>
  );
}
