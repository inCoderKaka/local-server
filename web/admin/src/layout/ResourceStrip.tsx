import { ErrorBanner, formatBytes, formatPercent, Spinner, type Schema } from '@local-server/shared';
import { memoryBudgetFrom, useSettings } from '../lib/settings';
import { SETTING } from '../lib/settingKeys';
import { useShell } from '../lib/shell';

type ResourceUse = Schema<'ResourceUse'>;

/** The four parts of a host-metrics sample, in the order the UI shows them (glossary names). */
export const PARTS = [
  ['total', 'Host total'],
  ['share_budget_slice', 'Share-budget slice'],
  ['other_apps', 'Other apps'],
  ['unattributed', 'Unattributed'],
] as const;

function Use({ use }: { use: ResourceUse }) {
  return (
    <>
      <span title={`Memory, measured by ${use.memory_bytes.method}`}>Memory {formatBytes(use.memory_bytes.value)}</span>
      <span aria-hidden="true"> · </span>
      <span title={`CPU, measured by ${use.cpu_percent.method}`}>CPU {formatPercent(use.cpu_percent.value)}</span>
    </>
  );
}

/**
 * The header strip shown on every page: host total, the share-budget slice with the memory
 * budget, other apps and unattributed use (req_001 D-31). A value that cannot be read is `n/a`;
 * the memory budget is shown only when the setting holds a number.
 */
export function ResourceStrip() {
  const { metrics } = useShell();
  const settings = useSettings();
  const budget = memoryBudgetFrom(settings.list);
  const m = metrics.data;
  return (
    <section aria-label="Host resources" className="border-t border-slate-200 bg-white px-4 py-2 text-xs text-slate-800">
      {!m && metrics.loading && <Spinner label="Loading host resources" />}
      {!m && metrics.error && <ErrorBanner error={metrics.error} title="Host resources could not be read" />}
      {m && (
        <dl className="flex flex-wrap gap-x-6 gap-y-1">
          {PARTS.map(([key, label]) => (
            <div key={key} className="flex gap-1">
              <dt className="font-semibold">{label}</dt>
              <dd>
                <Use use={m[key]} />
              </dd>
            </div>
          ))}
          {budget !== null && (
            <div className="flex gap-1">
              <dt className="font-semibold">Memory budget</dt>
              <dd title={`Setting ${SETTING.memoryBudgetBytes}`}>{budget === 0 ? 'no limit' : formatBytes(budget)}</dd>
            </div>
          )}
        </dl>
      )}
    </section>
  );
}
