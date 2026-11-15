import { useState } from 'react';
import { call, Card, ConfirmAction, ErrorBanner, formatDateTime, Spinner, Table, useApi, useClient, UserText, type Column } from '@local-server/shared';
import { IssueKeyForm, type IssuedKey } from './IssueKeyForm';
import { keyLabel, KeyStatusBadge, type AccessKey, type Consumer } from './labels';
import { ShownOnce } from './ShownOnce';

function boundNode(k: AccessKey) {
  if (!k.bound_node) return 'Any node';
  return <UserText text={k.bound_node_name ?? k.bound_node} />;
}

/**
 * A consumer's access keys by prefix and status, revoking one, and issuing one whose secret is
 * shown once (req_001 D-25, D-40). The secret is held only in this component's state.
 */
export function AccessKeys({ consumer }: { consumer: Consumer }) {
  const client = useClient();
  const [issued, setIssued] = useState<IssuedKey | null>(null);
  const keys = useApi(
    async () =>
      (await call(client.GET('/admin/v1/consumers/{consumer_id}/access-keys', { params: { path: { consumer_id: consumer.id } } }))).data,
    // A change of status (a deletion revokes every key) reads the keys again.
    [client, consumer.id, consumer.status],
  );

  async function revoke(k: AccessKey) {
    const revoked = await call(client.POST('/admin/v1/access-keys/{access_key_id}/revoke', { params: { path: { access_key_id: k.id } } }));
    keys.setData((keys.data ?? []).map((x) => (x.id === revoked.id ? revoked : x)));
  }

  const columns: Column<AccessKey>[] = [
    { key: 'prefix', header: 'Key', render: (k) => <code>{keyLabel(k)}</code> },
    { key: 'name', header: 'Name', render: (k) => <UserText text={k.name} /> },
    { key: 'status', header: 'Status', render: (k) => <KeyStatusBadge status={k.status} /> },
    { key: 'node', header: 'Bound node', render: boundNode },
    { key: 'created', header: 'Issued', render: (k) => formatDateTime(k.created_at) },
    { key: 'expires', header: 'Expires', render: (k) => (k.expires_at ? formatDateTime(k.expires_at) : 'Never') },
    { key: 'revoked', header: 'Revoked', render: (k) => (k.revoked_at ? formatDateTime(k.revoked_at) : '') },
    {
      key: 'actions',
      header: 'Actions',
      render: (k) =>
        k.status === 'active' ? (
          <ConfirmAction
            label="Revoke"
            name={`Revoke key ${keyLabel(k)}`}
            title={`Revoke key ${keyLabel(k)}?`}
            confirmLabel="Revoke key"
            onConfirm={() => revoke(k)}
          >
            Every call with this key is refused from now on. A revoked key cannot be used again.
          </ConfirmAction>
        ) : null,
    },
  ];

  return (
    <Card title="Access keys">
      <div className="flex flex-col gap-3">
        {issued && (
          <ShownOnce title="New access key" label="Access key secret" value={issued.secret} onDismiss={() => setIssued(null)}>
            Copy the key <code>{keyLabel(issued.access_key)}</code> now and give it to the consumer. It is shown only this once: the host keeps
            only its prefix and a hash, so it cannot show it again. If it is lost, revoke it and issue a new one.
          </ShownOnce>
        )}
        {consumer.status === 'suspended' && (
          <p className="text-sm text-amber-900">The consumer is suspended: every one of its keys is refused until it is reactivated.</p>
        )}
        <ErrorBanner error={keys.error} title="The access keys could not be read" />
        {keys.loading && !keys.data && <Spinner label="Loading access keys" />}
        {keys.data && <Table caption="Access keys, by prefix" columns={columns} rows={keys.data} rowKey={(k) => k.id} empty="No access keys yet." />}
        {consumer.status !== 'deleted' && (
          <IssueKeyForm
            consumerId={consumer.id}
            onIssued={(k) => {
              setIssued(k);
              void keys.refetch();
            }}
          />
        )}
      </div>
    </Card>
  );
}
