import { useId, useState } from 'react';
import { Button, call, Card, ErrorBanner, formatDateTime, Link, Spinner, Table, useClient, usePagedList, UserText, type Column } from '@local-server/shared';
import { CreateConsumerForm } from './CreateConsumerForm';
import { ConsumerName, ConsumerStatusBadge, kindLabel, type Consumer } from './labels';

type StatusFilter = '' | Consumer['status'];

const filters: { value: StatusFilter; label: string }[] = [
  { value: '', label: 'Active and suspended' },
  { value: 'active', label: 'Active' },
  { value: 'suspended', label: 'Suspended' },
  { value: 'deleted', label: 'Deleted' },
];

const columns: Column<Consumer>[] = [
  {
    key: 'name',
    header: 'Name',
    render: (c) => (
      <Link to={`/consumers/${encodeURIComponent(c.id)}`} className="text-sky-800 underline">
        <ConsumerName consumer={c} />
      </Link>
    ),
  },
  { key: 'kind', header: 'Kind', render: (c) => kindLabel[c.kind] },
  { key: 'status', header: 'Status', render: (c) => <ConsumerStatusBadge status={c.status} /> },
  { key: 'login', header: 'Login', render: (c) => (c.has_login ? 'Password or passkey' : 'Key only') },
  { key: 'email', header: 'Email', render: (c) => <UserText text={c.email} /> },
  { key: 'created', header: 'Created', render: (c) => formatDateTime(c.created_at) },
];

/** The consumers, newest first, a page at a time, and the form for a key-only consumer. */
export default function ConsumersPage() {
  const client = useClient();
  const filterId = useId();
  const [status, setStatus] = useState<StatusFilter>('');
  const list = usePagedList(
    (cursor) => call(client.GET('/admin/v1/consumers', { params: { query: { status: status || undefined, cursor } } })),
    [client, status],
  );

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Consumers</h1>
      <CreateConsumerForm />
      <Card
        title="All consumers"
        actions={
          <div className="flex items-center gap-2 text-sm">
            <label htmlFor={filterId}>Show</label>
            <select
              id={filterId}
              value={status}
              onChange={(e) => setStatus(e.target.value as StatusFilter)}
              className="rounded border border-slate-300 px-2 py-1"
            >
              {filters.map((f) => (
                <option key={f.value} value={f.value}>
                  {f.label}
                </option>
              ))}
            </select>
          </div>
        }
      >
        <div className="flex flex-col gap-3">
          <ErrorBanner error={list.error} title="The consumers could not be read" />
          {!list.loaded && list.loading && <Spinner label="Loading consumers" />}
          {list.loaded && (
            <Table caption="Consumers, newest first" columns={columns} rows={list.items} rowKey={(c) => c.id} empty="No consumers to show." />
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
    </div>
  );
}
