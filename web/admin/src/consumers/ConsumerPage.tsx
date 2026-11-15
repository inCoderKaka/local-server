import { useState, type ReactNode } from 'react';
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
  useApi,
  useClient,
  useParams,
  UserText,
  type ApiError,
} from '@local-server/shared';
import { AccessKeys } from './AccessKeys';
import { ConsumerName, ConsumerStatusBadge, kindLabel, type Consumer } from './labels';

function Details({ c }: { c: Consumer }) {
  const rows: [string, ReactNode][] = [
    ['Kind', kindLabel[c.kind]],
    ['Status', <ConsumerStatusBadge key="s" status={c.status} />],
    ['Login', c.has_login ? 'Password or passkey' : 'None: this consumer uses access keys only'],
    ['Login name', <UserText key="u" text={c.username} />],
    ['Email', <UserText key="e" text={c.email} />],
    ['Billing mode', c.billing_mode ?? ''],
    ['Created', formatDateTime(c.created_at)],
  ];
  if (c.deleted_at) rows.push(['Deleted', formatDateTime(c.deleted_at)]);
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="font-medium text-slate-700">{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Suspend, reactivate and delete. A deletion is confirmed first and cannot be undone (D-40, D-54). */
function Actions({ c, onChange, onDeleted }: { c: Consumer; onChange: (c: Consumer) => void; onDeleted: () => Promise<void> }) {
  const client = useClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const path = { params: { path: { consumer_id: c.id } } };

  async function setStatus(to: 'suspend' | 'reactivate') {
    setBusy(true);
    setError(null);
    try {
      onChange(
        await call(to === 'suspend' ? client.POST('/admin/v1/consumers/{consumer_id}/suspend', path) : client.POST('/admin/v1/consumers/{consumer_id}/reactivate', path)),
      );
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    await call(client.DELETE('/admin/v1/consumers/{consumer_id}', path));
    await onDeleted();
  }

  if (c.status === 'deleted') {
    return (
      <p className="text-sm text-slate-700">
        This consumer is deleted: its personal fields are erased and its keys are revoked. Its usage records stay. A deleted consumer cannot be
        reactivated.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <ErrorBanner error={error} />
      {c.status === 'active' ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={busy} onClick={() => void setStatus('suspend')}>
            Suspend
          </Button>
          <span className="text-slate-600">Blocks its keys and sign-ins until you reactivate it.</span>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="primary" disabled={busy} onClick={() => void setStatus('reactivate')}>
            Reactivate
          </Button>
          <span className="text-slate-600">Its keys and sign-ins work again.</span>
        </div>
      )}
      <div>
        <ConfirmAction label="Delete consumer" title="Delete this consumer?" confirmLabel="Delete for good" onConfirm={remove} disabled={busy}>
          <ul className="list-disc pl-5">
            <li>Its display name, email, login, key names and invite notes are erased, and its sign-ins end.</li>
            <li>Every one of its access keys is revoked at once.</li>
            <li>Its usage records stay, so past use still counts.</li>
            <li>This cannot be undone: a deleted consumer is never reactivated.</li>
          </ul>
        </ConfirmAction>
      </div>
    </div>
  );
}

/** One consumer: its details, the state actions and its access keys. */
export default function ConsumerPage() {
  const client = useClient();
  const { consumerId = '' } = useParams();
  const c = useApi(() => call(client.GET('/admin/v1/consumers/{consumer_id}', { params: { path: { consumer_id: consumerId } } })), [client, consumerId]);
  const consumer = c.data;

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">
        <Link to="/consumers" className="text-sky-800 underline">
          All consumers
        </Link>
      </p>
      <h1 className="text-xl font-semibold">{consumer ? <ConsumerName consumer={consumer} /> : 'Consumer'}</h1>
      <ErrorBanner error={c.error} title="The consumer could not be read" />
      {c.loading && !consumer && <Spinner label="Loading the consumer" />}
      {consumer && (
        <>
          <div className="grid gap-4 lg:grid-cols-2">
            <Card title="Details">
              <Details c={consumer} />
            </Card>
            <Card title="State">
              <Actions c={consumer} onChange={c.setData} onDeleted={c.refetch} />
            </Card>
          </div>
          <p className="text-sm">
            <Link to={`/consumers/${encodeURIComponent(consumer.id)}/grant`} className="text-sky-800 underline">
              Grant and rules: models, limits, quotas and history
            </Link>
          </p>
          <AccessKeys consumer={consumer} />
        </>
      )}
    </div>
  );
}
