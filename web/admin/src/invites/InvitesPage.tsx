import { Badge, Button, call, Card, ConfirmAction, ErrorBanner, formatDateTime, Link, Spinner, Table, useClient, usePagedList, UserText, type Column, type Schema } from '@local-server/shared';
import { CreateInviteForm } from './CreateInviteForm';

type Invite = Schema<'Invite'>;

const tone = { pending: 'info', redeemed: 'good', expired: 'neutral', revoked: 'bad' } as const;
const statusLabel: Record<Invite['status'], string> = { pending: 'Pending', redeemed: 'Redeemed', expired: 'Expired', revoked: 'Revoked' };

/** The invites, newest first, a page at a time; creating and revoking one. */
export default function InvitesPage() {
  const client = useClient();
  const list = usePagedList((cursor) => call(client.GET('/admin/v1/invites', { params: { query: { cursor } } })), [client]);

  async function revoke(i: Invite) {
    list.replace(await call(client.POST('/admin/v1/invites/{invite_id}/revoke', { params: { path: { invite_id: i.id } } })));
  }

  const columns: Column<Invite>[] = [
    { key: 'note', header: 'Note', render: (i) => <UserText text={i.note} /> },
    { key: 'status', header: 'Status', render: (i) => <Badge tone={tone[i.status]}>{statusLabel[i.status]}</Badge> },
    { key: 'created', header: 'Created', render: (i) => formatDateTime(i.created_at) },
    { key: 'expires', header: 'Expires', render: (i) => formatDateTime(i.expires_at) },
    {
      key: 'consumer',
      header: 'Consumer',
      render: (i) =>
        i.redeemed_consumer_id ? (
          <Link to={`/consumers/${encodeURIComponent(i.redeemed_consumer_id)}`} className="text-sky-800 underline">
            Open consumer
          </Link>
        ) : null,
    },
    {
      key: 'actions',
      header: 'Actions',
      render: (i) =>
        i.status === 'pending' ? (
          <ConfirmAction
            label="Revoke"
            name={`Revoke invite ${i.note ?? `created ${formatDateTime(i.created_at)}`}`}
            title="Revoke this invite?"
            confirmLabel="Revoke invite"
            onConfirm={() => revoke(i)}
          >
            Its link stops working at once. A revoked invite cannot be used again.
          </ConfirmAction>
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Invites</h1>
      <CreateInviteForm onCreated={() => void list.reload()} />
      <Card title="All invites">
        <div className="flex flex-col gap-3">
          <ErrorBanner error={list.error} title="The invites could not be read" />
          {!list.loaded && list.loading && <Spinner label="Loading invites" />}
          {list.loaded && <Table caption="Invites, newest first" columns={columns} rows={list.items} rowKey={(i) => i.id} empty="No invites yet." />}
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
