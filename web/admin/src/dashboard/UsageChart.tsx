import { useEffect, useState } from 'react';
import {
  Button,
  call,
  canDraw,
  Card,
  ErrorBanner,
  formatBytes,
  formatDateTime,
  formatPercent,
  Plot,
  Spinner,
  Table,
  useApi,
  useClient,
  type Series,
} from '@local-server/shared';
import type { HostMetricsSample } from '../lib/shell';

const colours = { total: '#334155', slice: '#0284c7', other: '#d97706' };

function seriesOf(samples: HostMetricsSample[], pick: (u: HostMetricsSample['total']) => number | null): Series[] {
  return [
    { label: 'Host total', values: samples.map((s) => pick(s.total)), stroke: colours.total },
    { label: 'Share-budget slice', values: samples.map((s) => pick(s.share_budget_slice)), stroke: colours.slice },
    { label: 'Other apps', values: samples.map((s) => pick(s.other_apps)), stroke: colours.other },
  ];
}

/**
 * Host use over time from GET /admin/v1/host-metrics/history. Without from and to the server
 * picks the range (admin.metrics_history_default_range). The same data is always available as a
 * table.
 */
export function UsageChart() {
  const client = useClient();
  const hist = useApi(() => call(client.GET('/admin/v1/host-metrics/history')), [client]);
  const [drawable] = useState(canDraw);
  const samples = hist.data?.data ?? [];
  // Stable inputs for the plots, so they redraw only when the data changes.
  const [chart, setChart] = useState<{ xs: number[]; mem: Series[]; cpu: Series[] } | null>(null);
  useEffect(() => {
    if (!hist.data) return;
    const s = hist.data.data;
    setChart({
      xs: s.map((x) => Date.parse(x.sampled_at) / 1000),
      mem: seriesOf(s, (u) => u.memory_bytes.value),
      cpu: seriesOf(s, (u) => u.cpu_percent.value),
    });
  }, [hist.data]);

  return (
    <Card title="Host use over time" actions={<Button onClick={() => void hist.refetch()}>Refresh</Button>}>
      <ErrorBanner error={hist.error} />
      {!hist.data && hist.loading && <Spinner />}
      {hist.data && samples.length === 0 && <p className="text-sm text-slate-600">No samples yet in this range.</p>}
      {chart && samples.length > 0 && drawable && (
        <div className="flex flex-col gap-4">
          <Plot title="Memory" xs={chart.xs} series={chart.mem} format={formatBytes} />
          <Plot title="CPU" xs={chart.xs} series={chart.cpu} format={formatPercent} />
        </div>
      )}
      {samples.length > 0 && !drawable && (
        <p className="text-sm text-slate-600">This browser cannot draw the chart; the table below has the same data.</p>
      )}
      {samples.length > 0 && (
        <details className="mt-3" open={!drawable}>
          <summary className="cursor-pointer text-sm">Show the data as a table</summary>
          <Table
            caption="Host use per minute, oldest first"
            rowKey={(s) => s.sampled_at}
            rows={samples}
            columns={[
              { key: 't', header: 'Time', render: (s) => formatDateTime(s.sampled_at) },
              { key: 'tm', header: 'Total memory', render: (s) => formatBytes(s.total.memory_bytes.value) },
              { key: 'sm', header: 'Slice memory', render: (s) => formatBytes(s.share_budget_slice.memory_bytes.value) },
              { key: 'om', header: 'Other apps memory', render: (s) => formatBytes(s.other_apps.memory_bytes.value) },
              { key: 'tc', header: 'Total CPU', render: (s) => formatPercent(s.total.cpu_percent.value) },
              { key: 'sc', header: 'Slice CPU', render: (s) => formatPercent(s.share_budget_slice.cpu_percent.value) },
              { key: 'oc', header: 'Other apps CPU', render: (s) => formatPercent(s.other_apps.cpu_percent.value) },
            ]}
          />
        </details>
      )}
    </Card>
  );
}
