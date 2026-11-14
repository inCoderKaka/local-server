import { Card, ErrorBanner, formatBytes, formatDateTime, formatPercent, Spinner, type Schema } from '@local-server/shared';
import { PARTS } from '../layout/ResourceStrip';
import { memoryBudgetFrom, useSettings } from '../lib/settings';
import { useShell } from '../lib/shell';

type MetricValue = Schema<'MetricValue'>;

/** One measured value with a meter against the host total, and how it was measured. */
function Value({ label, v, max, format }: { label: string; v: MetricValue; max: number | null; format: (n: number | null) => string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-lg font-semibold">{format(v.value)}</span>
      {v.value !== null && max !== null && max > 0 && (
        <meter aria-label={label} min={0} max={max} value={Math.min(v.value, max)} className="w-full" />
      )}
      <span className="text-xs text-slate-500">measured by {v.method}</span>
    </div>
  );
}

/** The current host use in full: memory and CPU of each part (req_001 D-31). */
export function HostUse() {
  const { metrics } = useShell();
  const budget = memoryBudgetFrom(useSettings().list);
  const m = metrics.data;
  return (
    <Card title="Host use now">
      <ErrorBanner error={metrics.error} />
      {!m && metrics.loading && <Spinner />}
      {m && (
        <>
          <table className="w-full text-left text-sm">
            <caption className="mb-2 text-left text-xs text-slate-600">Sampled {formatDateTime(m.sampled_at)}</caption>
            <thead>
              <tr>
                <th scope="col" className="py-1">
                  Part
                </th>
                <th scope="col" className="py-1">
                  Memory
                </th>
                <th scope="col" className="py-1">
                  CPU
                </th>
              </tr>
            </thead>
            <tbody>
              {PARTS.map(([key, label]) => (
                <tr key={key} className="border-t border-slate-100 align-top">
                  <th scope="row" className="py-2 pr-4 font-medium">
                    {label}
                  </th>
                  <td className="py-2 pr-4">
                    <Value label={`${label} memory`} v={m[key].memory_bytes} max={m.total.memory_bytes.value} format={formatBytes} />
                  </td>
                  <td className="py-2">
                    <Value label={`${label} CPU`} v={m[key].cpu_percent} max={100} format={formatPercent} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="mt-2 text-xs text-slate-600">
            {budget === null
              ? 'The memory budget setting could not be read.'
              : budget === 0
                ? 'Memory budget: no limit (scheduler.memory_budget_bytes is 0).'
                : `Memory budget: ${formatBytes(budget)} for loaded models and running requests together.`}
          </p>
        </>
      )}
    </Card>
  );
}
