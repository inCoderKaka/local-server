import type { ReactNode } from 'react';
import { Button, Card, ErrorBanner, formatCount, formatDateTime, formatSeconds, Link, Spinner, Table, UserText, type Column, type Schema } from '@local-server/shared';
import { billingText, limitText, metricLabel, overflowText, RequestTypes, useMyLimits, type GrantVersion, type QuotaEntry } from '../lib/limits';
import { REQUESTS_PATH, requestHref } from '../lib/requests';
import { useSession } from '../lib/session';

type ModelLimit = Schema<'GrantModelLimit'>;

const modelColumns: Column<ModelLimit>[] = [
  { key: 'model', header: 'Model', render: (m) => <UserText text={m.model} /> },
  { key: 'context', header: 'Longest prompt and answer (context limit)', render: (m) => limitText(m.context_limit, 'tokens') },
  { key: 'output', header: 'Most tokens one answer may have', render: (m) => limitText(m.max_output_tokens, 'tokens') },
];

const quotaColumns: Column<QuotaEntry>[] = [
  { key: 'metric', header: 'Counts', render: (q) => metricLabel[q.metric] },
  { key: 'limit', header: 'At most', render: (q) => formatCount(q.limit) },
  { key: 'window', header: 'Every', render: (q) => formatSeconds(q.window_seconds) },
];

function Terms({ v }: { v: GrantVersion }) {
  const { consumer } = useSession();
  const l = v.limits;
  const rows: [string, ReactNode][] = [
    ['Requests at once', `Up to ${formatCount(l.concurrency)} of your calls run at the same time; more wait or are refused.`],
    ['If a prompt is too long', overflowText[l.on_context_overflow]],
    ['Allowed request types', <RequestTypes key="t" allowed={l.allowed_request_properties} />],
    ['Your access ends', l.expires_at ? formatDateTime(l.expires_at) : 'Never: it lasts until the host changes it.'],
    ['Billing', billingText[consumer.billing_mode ?? 'none']],
    [
      'Set',
      <span key="s">
        Version {v.version}, {formatDateTime(v.created_at)}
        {v.resource_request_id ? (
          <>
            , when the host approved{' '}
            <Link to={requestHref(v.resource_request_id)} className="text-sky-800 underline">
              your request
            </Link>
          </>
        ) : null}
        .
      </span>,
    ],
  ];
  return (
    <div className="flex flex-col gap-4">
      <Card title="Models you may use">
        <Table caption="Models and their limits" columns={modelColumns} rows={l.models} rowKey={(m) => m.model} empty="No models: every call is refused." />
      </Card>
      <Card title="Quotas">
        <Table
          caption="How much you may use in each window; the count starts again with each new window"
          columns={quotaColumns}
          rows={l.quotas ?? []}
          rowKey={(q) => `${q.metric}-${q.window_seconds}`}
          empty="No quota: your use is not capped by a quota."
        />
      </Card>
      <Card title="Other terms">
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
          {rows.map(([k, val]) => (
            <div key={k} className="contents">
              <dt className="font-medium text-slate-700">{k}</dt>
              <dd>{val}</dd>
            </div>
          ))}
        </dl>
      </Card>
    </div>
  );
}

/**
 * My limits: the consumer's current grant version (GET /v1/me/limits) in plain language. A key the
 * host issued may narrow these for its own calls. Without a grant the page says there is no access
 * yet and offers a resource request (req_001 D-150).
 */
export default function LimitsPage() {
  const limits = useMyLimits();
  return (
    <>
      <h1 className="text-xl font-semibold">My limits</h1>
      <ErrorBanner error={limits.error} title="Your limits could not be read" />
      {limits.loading && limits.data === undefined && <Spinner label="Loading your limits" />}
      {limits.data === null && (
        <Card title="No access yet">
          <p className="text-sm">
            The host has not given this account any limits yet, so no call is allowed. <Link to={REQUESTS_PATH}>Ask the host for access</Link> with a
            resource request.
          </p>
        </Card>
      )}
      {limits.data && (
        <>
          <p className="text-sm text-slate-700">
            These are the terms the host set for your account. A key can have lower limits of its own; ask for more on the{' '}
            <Link to={REQUESTS_PATH}>Requests</Link> page.
          </p>
          <Terms v={limits.data} />
        </>
      )}
      {limits.data !== undefined && (
        <div>
          <Button onClick={() => void limits.refetch()}>Refresh</Button>
        </div>
      )}
    </>
  );
}
