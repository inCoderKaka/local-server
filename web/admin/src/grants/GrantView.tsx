import type { ReactNode } from 'react';
import { formatDateTime, Table, UserText, type Column } from '@local-server/shared';
import { limitText, metricText, overflowText, RequestTypes, type GrantModelLimit, type GrantVersion, type QuotaEntry } from './labels';
import { formatSeconds } from './values';

const modelColumns: Column<GrantModelLimit>[] = [
  { key: 'model', header: 'Model', render: (m) => <UserText text={m.model} /> },
  { key: 'context', header: 'Context limit (tokens)', render: (m) => limitText(m.context_limit) },
  { key: 'output', header: 'Most output tokens per call', render: (m) => limitText(m.max_output_tokens) },
];

const quotaColumns: Column<QuotaEntry>[] = [
  { key: 'metric', header: 'Counts', render: (q) => metricText[q.metric] },
  { key: 'limit', header: 'At most', render: (q) => String(q.limit) },
  { key: 'window', header: 'Every', render: (q) => formatSeconds(q.window_seconds) },
];

/** The terms of one grant version, read only. */
export function GrantView({ version }: { version: GrantVersion }) {
  const l = version.limits;
  const rows: [string, ReactNode][] = [
    ['Version', `${version.version}, set ${formatDateTime(version.created_at)}${version.resource_request_id ? ', by approving a resource request' : ''}`],
    ['Requests at once', String(l.concurrency)],
    ['If a prompt is too long', `${overflowText[l.on_context_overflow].label}. ${overflowText[l.on_context_overflow].help}`],
    ['Allowed request types', <RequestTypes key="p" allowed={l.allowed_request_properties} />],
    ['Expires', l.expires_at ? formatDateTime(l.expires_at) : 'Never'],
  ];
  return (
    <div className="flex flex-col gap-3">
      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="font-medium text-slate-700">{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
      <Table caption="Models of this grant" columns={modelColumns} rows={l.models} rowKey={(m) => m.model} empty="No models: every call is refused." />
      <Table
        caption="Quotas of this grant"
        columns={quotaColumns}
        rows={l.quotas ?? []}
        rowKey={(q) => `${q.metric}-${q.window_seconds}`}
        empty="No quota: use is not capped by a quota."
      />
    </div>
  );
}
