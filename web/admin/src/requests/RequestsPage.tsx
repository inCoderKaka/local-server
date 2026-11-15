import { useEffect, useId, useMemo, useState, type ReactNode } from 'react';
import {
  asApiError,
  Button,
  call,
  Card,
  ConfirmAction,
  ErrorBanner,
  formatDateTime,
  Link,
  secondsFrom,
  Spinner,
  Table,
  TextArea,
  useApi,
  useClient,
  usePagedList,
  UserText,
  type ApiClient,
  type ApiError,
  type Column,
  type Schema,
} from '@local-server/shared';
import { ConsumerName } from '../consumers/labels';
import { GrantForm } from '../grants/GrantForm';
import type { GrantLimits, GrantVersion } from '../grants/labels';
import { HOST_SETTING, intSetting, textSetting } from '../grants/values';
import { useSettings } from '../lib/settings';
import { useShell } from '../lib/shell';
import { limitsFromAsk, type ResourceRequest, type ResourceRequestStatus } from './ask';
import { Author, AskSummary, RequestStatusBadge, statusText } from './labels';

type Consumer = Schema<'Consumer'>;
type Filter = '' | ResourceRequestStatus;

const filters: { value: Filter; label: string }[] = [
  { value: 'open', label: statusText.open },
  { value: 'approved', label: statusText.approved },
  { value: 'rejected', label: statusText.rejected },
  { value: 'withdrawn', label: statusText.withdrawn },
  { value: 'expired', label: statusText.expired },
  { value: '', label: 'All' },
];

const consumerHref = (id: string) => `/consumers/${encodeURIComponent(id)}`;

/** The host's quota window in seconds (limits.quota_window), or null when it is not readable. */
function useQuotaWindowSeconds(): number | null {
  const { list } = useSettings();
  const w = textSetting(list, HOST_SETTING.quotaWindow);
  return w ? (secondsFrom(w).value ?? null) : null;
}

/**
 * The consumers the requests name, read once each (GET /admin/v1/consumers/{id}); the list of
 * requests carries only the id. A consumer that cannot be read is null and shows by its id.
 */
function useConsumers(client: ApiClient, ids: string[]): Map<string, Consumer | null> {
  const [known, setKnown] = useState<Map<string, Consumer | null>>(() => new Map());
  const missing = ids.filter((id) => !known.has(id));
  const key = [...new Set(missing)].sort().join(',');
  useEffect(() => {
    if (!key) return;
    let live = true;
    void Promise.all(
      key.split(',').map(async (id): Promise<[string, Consumer | null]> => {
        try {
          return [id, await call(client.GET('/admin/v1/consumers/{consumer_id}', { params: { path: { consumer_id: id } } }))];
        } catch {
          return [id, null];
        }
      }),
    ).then((entries) => {
      if (live) setKnown((m) => new Map([...m, ...entries]));
    });
    return () => {
      live = false;
    };
  }, [client, key]);
  return known;
}

function Who({ id, consumer }: { id: string; consumer: Consumer | null | undefined }) {
  return (
    <Link to={consumerHref(id)} className="text-sky-800 underline">
      {consumer ? <ConsumerName consumer={consumer} /> : <span className="font-mono text-xs">{id}</span>}
    </Link>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex flex-col gap-2 border-t border-slate-200 pt-3">
      <h3 id={id} className="text-sm font-semibold text-slate-900">
        {title}
      </h3>
      {children}
    </section>
  );
}

/** What happened after a decision on this page, shown above the request. */
type Outcome = { status: ResourceRequestStatus; warnings: string[] };

/**
 * Sends a write and hands its answer on. A request that reached a terminal state meanwhile
 * answers 409: that is reported to `onConflict`, which reads the request again, and is not
 * thrown, so the form it came from does not show it a second time.
 */
async function write(p: Promise<ResourceRequest>, onDone: (r: ResourceRequest) => void, onConflict: (e: ApiError) => Promise<void>) {
  try {
    onDone(await p);
  } catch (e) {
    const err = asApiError(e);
    if (err.status === 409) return onConflict(err);
    throw err;
  }
}

function CommentForm({ id, onDone, onConflict }: { id: string; onDone: (r: ResourceRequest) => void; onConflict: (e: ApiError) => Promise<void> }) {
  const client = useClient();
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  async function send() {
    setBusy(true);
    setError(null);
    try {
      await write(
        call(client.POST('/admin/v1/resource-requests/{resource_request_id}/comments', { params: { path: { resource_request_id: id } }, body: { body } })),
        (r) => {
          setBody('');
          onDone(r);
        },
        onConflict,
      );
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <ErrorBanner error={error} title="The comment was not added" />
      <TextArea label="Your comment" hint="The consumer sees it with the request." value={body} onChange={(e) => setBody(e.target.value)} />
      <div>
        <Button disabled={busy || body.trim() === ''} onClick={() => void send()}>
          Add comment
        </Button>
      </div>
    </div>
  );
}

function ApproveForm({ r, onDone, onConflict }: { r: ResourceRequest; onDone: (r: ResourceRequest) => void; onConflict: (e: ApiError) => Promise<void> }) {
  const client = useClient();
  const settings = useSettings();
  const quotaWindowSeconds = useQuotaWindowSeconds();
  const [comment, setComment] = useState('');
  const path = { params: { path: { consumer_id: r.consumer_id } } };
  const grant = useApi(async (): Promise<GrantVersion | null> => {
    try {
      return await call(client.GET('/admin/v1/consumers/{consumer_id}/grant', path));
    } catch (e) {
      // A 404 is a consumer with no grant yet: the approval sets its first one.
      const err = asApiError(e);
      if (err.status === 404) return null;
      throw err;
    }
  }, [client, r.consumer_id]);
  const models = useApi(async () => (await call(client.GET('/admin/v1/models'))).data, [client]);
  const defaultConcurrency = intSetting(settings.list, HOST_SETTING.grantConcurrency);
  const start = useMemo(
    () => (grant.data === undefined ? undefined : limitsFromAsk(grant.data?.limits ?? null, r.ask, { defaultConcurrency, quotaWindowSeconds })),
    [grant.data, r.ask, defaultConcurrency, quotaWindowSeconds],
  );

  async function send(limits: GrantLimits) {
    const c = comment.trim();
    await write(
      call(
        client.POST('/admin/v1/resource-requests/{resource_request_id}/approve', {
          params: { path: { resource_request_id: r.id } },
          body: { limits, ...(c ? { comment } : {}) },
        }),
      ),
      onDone,
      onConflict,
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-slate-700">
        Approving replaces the consumer's grant with the terms below. They start from the current grant with what the request asks for applied;
        change any of them first.
      </p>
      <ErrorBanner error={grant.error} title="The consumer's grant could not be read" />
      <ErrorBanner error={models.error} title="The host's models could not be read" />
      {grant.loading && grant.data === undefined && <Spinner label="Loading the consumer's grant" />}
      {start && (
        <GrantForm
          key={grant.data?.id ?? 'none'}
          consumerId={r.consumer_id}
          current={grant.data ?? null}
          hostModels={models.data}
          start={start}
          send={send}
          label="Approve the request"
          submitLabel="Approve"
        >
          <TextArea label="Comment for the consumer (optional)" value={comment} onChange={(e) => setComment(e.target.value)} />
        </GrantForm>
      )}
    </div>
  );
}

function RejectForm({ id, onDone, onConflict }: { id: string; onDone: (r: ResourceRequest) => void; onConflict: (e: ApiError) => Promise<void> }) {
  const client = useClient();
  const [comment, setComment] = useState('');
  return (
    <div className="flex flex-col gap-2">
      <TextArea label="Reason for the consumer (optional)" value={comment} onChange={(e) => setComment(e.target.value)} />
      <div>
        <ConfirmAction
          label="Reject"
          title="Reject this request?"
          confirmLabel="Reject the request"
          onConfirm={() =>
            write(
              call(
                client.POST('/admin/v1/resource-requests/{resource_request_id}/reject', {
                  params: { path: { resource_request_id: id } },
                  body: comment.trim() ? { comment } : {},
                }),
              ),
              onDone,
              onConflict,
            )
          }
        >
          The consumer's grant stays as it is. A rejected request cannot be opened again; the consumer can send a new one.
        </ConfirmAction>
      </div>
    </div>
  );
}

/** One request with its comment thread and, while it is open, the comment, approve and reject actions. */
function RequestDetail({
  id,
  consumers,
  onChanged,
  onClose,
}: {
  id: string;
  consumers: Map<string, Consumer | null>;
  onChanged: () => void;
  onClose: () => void;
}) {
  const client = useClient();
  const quotaWindowSeconds = useQuotaWindowSeconds();
  const req = useApi(
    () => call(client.GET('/admin/v1/resource-requests/{resource_request_id}', { params: { path: { resource_request_id: id } } })),
    [client, id],
  );
  const [conflict, setConflict] = useState<ApiError | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const r = req.data;

  function done(next: ResourceRequest) {
    // A write answers with the whole request, comments included; warnings come only with an approval.
    req.setData(next);
    setConflict(null);
    if (next.status !== 'open') setOutcome({ status: next.status, warnings: next.warnings ?? [] });
    onChanged();
  }

  async function conflicted(e: ApiError) {
    setOutcome(null);
    setConflict(e);
    await req.refetch();
    onChanged();
  }

  return (
    <Card title="Request" actions={<Button onClick={onClose}>Close</Button>}>
      <div className="flex flex-col gap-3 text-sm">
        <ErrorBanner error={req.error} title="The request could not be read" />
        {req.loading && !r && <Spinner label="Loading the request" />}
        {conflict && (
          <ErrorBanner
            error={conflict}
            title="This request was already decided, withdrawn or expired, so nothing was changed. It is shown again as it is now."
          />
        )}
        {outcome && (
          <div role="status" className="rounded border border-emerald-300 bg-emerald-50 px-3 py-2 text-emerald-950">
            <p>
              {outcome.status === 'approved' ? 'Approved: the consumer has a new grant version.' : `The request is now ${statusText[outcome.status].toLowerCase()}.`}
            </p>
            {outcome.warnings.length > 0 && (
              <>
                <p className="mt-1 font-medium">The host noted:</p>
                <ul className="list-disc pl-5">
                  {outcome.warnings.map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              </>
            )}
          </div>
        )}
        {r && (
          <>
            <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1">
              <dt className="font-medium text-slate-700">Status</dt>
              <dd>
                <RequestStatusBadge status={r.status} />
              </dd>
              <dt className="font-medium text-slate-700">Consumer</dt>
              <dd>
                <Who id={r.consumer_id} consumer={consumers.get(r.consumer_id)} /> ·{' '}
                <Link to={`${consumerHref(r.consumer_id)}/grant`} className="text-sky-800 underline">
                  Grant
                </Link>
              </dd>
              <dt className="font-medium text-slate-700">Sent</dt>
              <dd>{formatDateTime(r.created_at)}</dd>
              {r.status === 'open' && r.expires_at && (
                <>
                  <dt className="font-medium text-slate-700">Expires</dt>
                  <dd>{formatDateTime(r.expires_at)}</dd>
                </>
              )}
              {r.decided_at && (
                <>
                  <dt className="font-medium text-slate-700">Closed</dt>
                  <dd>{formatDateTime(r.decided_at)}</dd>
                </>
              )}
              {r.resulting_grant_version_id && (
                <>
                  <dt className="font-medium text-slate-700">Grant version</dt>
                  <dd className="font-mono text-xs">{r.resulting_grant_version_id}</dd>
                </>
              )}
            </dl>
            <Section title="Message">
              <p>
                <UserText text={r.message} />
              </p>
            </Section>
            <Section title="Asks for">
              <AskSummary ask={r.ask} quotaWindowSeconds={quotaWindowSeconds} empty="Nothing in particular: see the message." />
            </Section>
            {r.warnings && r.warnings.length > 0 && !outcome && (
              <Section title="Ignored in the request">
                <ul className="list-disc pl-5">
                  {r.warnings.map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              </Section>
            )}
            <Section title="Comments">
              {(r.comments ?? []).length === 0 ? (
                <p className="text-slate-600">No comments yet.</p>
              ) : (
                <ol aria-label="Comments, oldest first" className="flex flex-col gap-2">
                  {(r.comments ?? []).map((c) => (
                    <li key={c.id} className="rounded border border-slate-200 p-2">
                      <p className="flex flex-wrap items-baseline gap-2">
                        <Author author={c.author} />
                        <span className="text-xs text-slate-600">{formatDateTime(c.created_at)}</span>
                      </p>
                      <p>
                        <UserText text={c.body} />
                      </p>
                    </li>
                  ))}
                </ol>
              )}
              {r.status === 'open' && <CommentForm id={r.id} onDone={done} onConflict={conflicted} />}
            </Section>
            {r.status === 'open' && (
              <>
                <Section title="Approve">
                  <ApproveForm r={r} onDone={done} onConflict={conflicted} />
                </Section>
                <Section title="Reject">
                  <RejectForm id={r.id} onDone={done} onConflict={conflicted} />
                </Section>
              </>
            )}
          </>
        )}
      </div>
    </Card>
  );
}

/**
 * The resource requests consumers sent, newest first, filtered by state (pending by default), with
 * one request's comments and the approve, reject and comment actions (req_001 D-39). The pending
 * count is the shell's, polled every admin.ui_poll_interval (D-38, D-157). A host that does not
 * have resource requests yet answers 501, shown as a calm note, not an error.
 */
export default function RequestsPage() {
  const client = useClient();
  const { pending } = useShell();
  const quotaWindowSeconds = useQuotaWindowSeconds();
  const filterId = useId();
  const [status, setStatus] = useState<Filter>('open');
  const [selected, setSelected] = useState<string | null>(null);
  const list = usePagedList(
    (cursor) => call(client.GET('/admin/v1/resource-requests', { params: { query: { status: status || undefined, cursor } } })),
    [client, status],
  );
  const consumers = useConsumers(
    client,
    list.items.map((r) => r.consumer_id),
  );
  const unavailable = list.error?.status === 501;

  function refresh() {
    void list.reload();
    void pending.refetch();
  }

  const columns: Column<ResourceRequest>[] = [
    { key: 'who', header: 'Consumer', render: (r) => <Who id={r.consumer_id} consumer={consumers.get(r.consumer_id)} /> },
    { key: 'status', header: 'Status', render: (r) => <RequestStatusBadge status={r.status} /> },
    { key: 'ask', header: 'Asks for', render: (r) => <AskSummary ask={r.ask} quotaWindowSeconds={quotaWindowSeconds} empty="See the message" /> },
    { key: 'sent', header: 'Sent', render: (r) => formatDateTime(r.created_at) },
    {
      key: 'open',
      header: <span className="sr-only">Details</span>,
      render: (r) => (
        <Button
          aria-label={`Show the request from ${consumers.get(r.consumer_id)?.display_name ?? r.consumer_id}, sent ${formatDateTime(r.created_at)}`}
          aria-pressed={selected === r.id}
          onClick={() => setSelected(r.id)}
        >
          Show
        </Button>
      ),
    },
  ];

  const count = pending.data?.kind === 'count' ? pending.data.n : null;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-baseline gap-3">
        <h1 className="text-xl font-semibold">Requests</h1>
        {count !== null && !unavailable && (
          <p className="text-sm text-slate-700">
            {count} pending
          </p>
        )}
      </div>
      {unavailable ? (
        <Card title="Not available yet">
          <p className="text-sm text-slate-700">This host does not take resource requests yet. Once it does, consumers' requests show here.</p>
        </Card>
      ) : (
        <>
          <p className="text-sm text-slate-700">
            Consumers ask here for more: another model, a higher quota or more requests at once. Approving sets a new grant version; rejecting leaves
            the grant as it is. Both can carry a comment for the consumer.
          </p>
          <Card
            title="Requests, newest first"
            actions={
              <div className="flex items-center gap-2 text-sm">
                <label htmlFor={filterId}>Show</label>
                <select
                  id={filterId}
                  value={status}
                  onChange={(e) => {
                    setStatus(e.target.value as Filter);
                    setSelected(null);
                  }}
                  className="rounded border border-slate-300 px-2 py-1"
                >
                  {filters.map((f) => (
                    <option key={f.value} value={f.value}>
                      {f.label}
                    </option>
                  ))}
                </select>
                <Button onClick={refresh}>Refresh</Button>
              </div>
            }
          >
            <div className="flex flex-col gap-3">
              <ErrorBanner error={list.error} title="The requests could not be read" />
              {!list.loaded && list.loading && <Spinner label="Loading the requests" />}
              {list.loaded && (
                <Table
                  caption="Resource requests"
                  columns={columns}
                  rows={list.items}
                  rowKey={(r) => r.id}
                  empty={status === 'open' ? 'No pending requests.' : 'No requests to show.'}
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
          {selected && (
            <RequestDetail
              key={selected}
              id={selected}
              consumers={consumers}
              onChanged={refresh}
              onClose={() => setSelected(null)}
            />
          )}
        </>
      )}
    </div>
  );
}
