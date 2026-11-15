import { useId, useState, type ReactNode } from 'react';
import {
  asApiError,
  Button,
  call,
  Card,
  ConfirmAction,
  ErrorBanner,
  formatDateTime,
  Link,
  Spinner,
  TextArea,
  useApi,
  useClient,
  UserText,
  useParams,
  type ApiError,
  type Schema,
} from '@local-server/shared';
import { Author, AskSummary, REQUESTS_PATH, RequestStatusBadge, statusText, type ResourceRequest } from '../lib/requests';

type Comment = Schema<'Comment'>;

function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex flex-col gap-2 border-t border-slate-200 pt-3">
      <h2 id={id} className="text-sm font-semibold text-slate-900">
        {title}
      </h2>
      {children}
    </section>
  );
}

/**
 * The host's note on its decision: the host's last comment written when it decided (an approval or
 * a rejection can carry one), if any.
 */
function decisionNote(r: ResourceRequest): Comment | null {
  if (!r.decided_at) return null;
  const at = Date.parse(r.decided_at);
  const host = (r.comments ?? []).filter((c) => c.author.kind !== 'consumer' && Date.parse(c.created_at) >= at);
  return host.length > 0 ? host[host.length - 1]! : null;
}

/** What the host decided, in plain words, with its note. */
function Decision({ r }: { r: ResourceRequest }) {
  const note = decisionNote(r);
  let text: ReactNode;
  switch (r.status) {
    case 'open':
      return null;
    case 'approved':
      text = (
        <>
          The host approved this request and set new limits for your account. <Link to="/limits">See your limits</Link>.
        </>
      );
      break;
    case 'rejected':
      text = 'The host rejected this request. Your limits stay as they were; you can send a new request.';
      break;
    case 'withdrawn':
      text = 'You withdrew this request.';
      break;
    case 'expired':
      text = 'The host did not answer this request in time, so it expired. You can send a new one.';
      break;
  }
  return (
    <div className="rounded border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
      <p>{text}</p>
      {note && (
        <p className="mt-1">
          <span className="font-medium">The host&apos;s note:</span> <UserText text={note.body} />
        </p>
      )}
    </div>
  );
}

/**
 * Sends a write and hands its answer on. A request that reached a final state meanwhile (the host
 * decided it, or it expired) answers 409: that is reported to `onConflict`, which reads the request
 * again, and not thrown.
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
        call(client.POST('/v1/resource-requests/{resource_request_id}/comments', { params: { path: { resource_request_id: id } }, body: { body } })),
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
      <TextArea label="Your comment" hint="The host sees it with the request." value={body} onChange={(e) => setBody(e.target.value)} />
      <div>
        <Button disabled={busy || body.trim() === ''} onClick={() => void send()}>
          Add comment
        </Button>
      </div>
    </div>
  );
}

/**
 * One of the consumer's requests (GET /v1/resource-requests/{id}): its state, what it asks for,
 * the host's decision and note, the comment thread, and while it is pending a comment form and
 * withdraw. Comments and the message are the writers' own text, shown only as text (req_001 D-31).
 */
export default function RequestPage() {
  const client = useClient();
  const { requestId = '' } = useParams();
  const path = { params: { path: { resource_request_id: requestId } } };
  const req = useApi(() => call(client.GET('/v1/resource-requests/{resource_request_id}', path)), [client, requestId]);
  const [conflict, setConflict] = useState<ApiError | null>(null);
  const [changed, setChanged] = useState('');
  const r = req.data;

  function done(next: ResourceRequest, what: string) {
    req.setData(next);
    setConflict(null);
    setChanged(what);
  }

  async function conflicted(e: ApiError) {
    setChanged('');
    setConflict(e);
    await req.refetch();
  }

  return (
    <>
      <p className="text-sm">
        <Link to={REQUESTS_PATH}>Back to your requests</Link>
      </p>
      <h1 className="text-xl font-semibold">Request</h1>
      <Card title="Request details" actions={<Button onClick={() => void req.refetch()}>Refresh</Button>}>
        <div className="flex flex-col gap-3 text-sm">
          <ErrorBanner error={req.error} title="The request could not be read" />
          {req.loading && !r && <Spinner label="Loading the request" />}
          {conflict && (
            <ErrorBanner error={conflict} title="This request was already decided or expired, so nothing was changed. It is shown again as it is now." />
          )}
          <p role="status" aria-live="polite" className="text-emerald-900">
            {changed}
          </p>
          {r && (
            <>
              <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1">
                <dt className="font-medium text-slate-700">Status</dt>
                <dd>
                  <RequestStatusBadge status={r.status} />
                </dd>
                <dt className="font-medium text-slate-700">Sent</dt>
                <dd>{formatDateTime(r.created_at)}</dd>
                {r.status === 'open' && r.expires_at && (
                  <>
                    <dt className="font-medium text-slate-700">Expires</dt>
                    <dd>{formatDateTime(r.expires_at)}, unless the host answers first</dd>
                  </>
                )}
                {r.decided_at && (
                  <>
                    <dt className="font-medium text-slate-700">{statusText[r.status]}</dt>
                    <dd>{formatDateTime(r.decided_at)}</dd>
                  </>
                )}
              </dl>
              <Decision r={r} />
              <Section title="Your message">
                <p>
                  <UserText text={r.message} />
                </p>
              </Section>
              <Section title="Asks for">
                <AskSummary ask={r.ask} empty="Nothing in particular: see the message." />
              </Section>
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
                {r.status === 'open' && <CommentForm id={r.id} onDone={(next) => done(next, 'Your comment was added.')} onConflict={conflicted} />}
              </Section>
              {r.status === 'open' && (
                <Section title="Withdraw">
                  <div>
                    <ConfirmAction
                      label="Withdraw"
                      title="Withdraw this request?"
                      confirmLabel="Withdraw the request"
                      onConfirm={() =>
                        write(
                          call(client.POST('/v1/resource-requests/{resource_request_id}/withdraw', path)),
                          (next) => done(next, 'The request is withdrawn.'),
                          conflicted,
                        )
                      }
                    >
                      The host no longer sees it as pending. A withdrawn request cannot be opened again; you can send a new one.
                    </ConfirmAction>
                  </div>
                </Section>
              )}
            </>
          )}
        </div>
      </Card>
    </>
  );
}
