import { useId, useState } from 'react';
import { Button, call, Card, ErrorBanner, formatDateTime, Link, Spinner, Table, useClient, usePagedList, type Column } from '@local-server/shared';
import { useMyLimits } from '../lib/limits';
import { AskSummary, RequestStatusBadge, requestHref, statusText, type ResourceRequest, type ResourceRequestStatus } from '../lib/requests';
import { useSession } from '../lib/session';
import { CreateRequestForm } from './CreateRequestForm';

type Filter = '' | ResourceRequestStatus;

const filters: { value: Filter; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'open', label: statusText.open },
  { value: 'approved', label: statusText.approved },
  { value: 'rejected', label: statusText.rejected },
  { value: 'withdrawn', label: statusText.withdrawn },
  { value: 'expired', label: statusText.expired },
];

const columns: Column<ResourceRequest>[] = [
  { key: 'sent', header: 'Sent', render: (r) => formatDateTime(r.created_at) },
  { key: 'status', header: 'Status', render: (r) => <RequestStatusBadge status={r.status} /> },
  { key: 'ask', header: 'Asks for', render: (r) => <AskSummary ask={r.ask} empty="See the message" /> },
  {
    key: 'open',
    header: <span className="sr-only">Details</span>,
    render: (r) => (
      <Link to={requestHref(r.id)} className="text-sky-800 underline" aria-label={`Open the request sent ${formatDateTime(r.created_at)}`}>
        Open
      </Link>
    ),
  },
];

/** What the last request sent from this page answered: the new request and what the host ignored. */
function Sent({ r }: { r: ResourceRequest }) {
  const warnings = r.warnings ?? [];
  return (
    <div role="status" className="rounded border border-emerald-300 bg-emerald-50 px-3 py-2 text-sm text-emerald-950">
      <p>
        Your request was sent. The host is told and answers it on its own time.{' '}
        <Link to={requestHref(r.id)} className="underline">
          Open the request
        </Link>
        .
      </p>
      {warnings.length > 0 && (
        <>
          <p className="mt-1 font-medium">The host ignored some of what you sent:</p>
          <ul className="list-disc pl-5">
            {warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

/**
 * Resource requests: the consumer's requests, newest first, filtered by state, and the form to ask
 * for more. A suspended account can read its requests and comment on open ones but cannot send a
 * new one (req_001 D-155). The page size is the host's.
 */
export default function RequestsPage() {
  const client = useClient();
  const { consumer } = useSession();
  const filterId = useId();
  const [status, setStatus] = useState<Filter>('');
  const [sent, setSent] = useState<ResourceRequest | null>(null);
  const limits = useMyLimits();
  const list = usePagedList(
    (cursor) => call(client.GET('/v1/resource-requests', { params: { query: { status: status || undefined, cursor } } })),
    [client, status],
  );
  const suspended = consumer.status === 'suspended';

  return (
    <>
      <h1 className="text-xl font-semibold">Requests</h1>
      <p className="text-sm text-slate-700">
        Ask the host here for more: another model, a longer context, a higher quota or more requests at once. The host approves or rejects each
        request, and either of you can comment while it is pending. A pending request expires after the host&apos;s own time limit.
      </p>
      {sent && <Sent r={sent} />}
      {suspended ? (
        <Card title="Ask the host for more">
          <p className="text-sm">The host has suspended this account, so you cannot send a new request. You can still read and comment on your requests.</p>
        </Card>
      ) : limits.data === undefined && limits.loading ? (
        <Spinner label="Loading the form" />
      ) : (
        <CreateRequestForm
          current={limits.data?.limits}
          onCreated={(r) => {
            setSent(r);
            void list.reload();
          }}
        />
      )}
      <Card
        title="Your requests, newest first"
        actions={
          <div className="flex items-center gap-2 text-sm">
            <label htmlFor={filterId}>Show</label>
            <select id={filterId} value={status} onChange={(e) => setStatus(e.target.value as Filter)} className="rounded border border-slate-300 px-2 py-1">
              {filters.map((f) => (
                <option key={f.value} value={f.value}>
                  {f.label}
                </option>
              ))}
            </select>
            <Button onClick={() => void list.reload()}>Refresh</Button>
          </div>
        }
      >
        <div className="flex flex-col gap-3">
          <ErrorBanner error={list.error} title="Your requests could not be read" />
          {!list.loaded && list.loading && <Spinner label="Loading your requests" />}
          {list.loaded && (
            <Table
              caption="Your resource requests"
              columns={columns}
              rows={list.items}
              rowKey={(r) => r.id}
              empty={status === '' ? 'You have not sent a request yet.' : 'No requests in this state.'}
            />
          )}
          {list.next && (
            <div>
              <Button disabled={list.loading} onClick={() => void list.loadMore()}>
                Show more
              </Button>
            </div>
          )}
        </div>
      </Card>
    </>
  );
}
