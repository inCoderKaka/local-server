import { Badge, Button, call, Card, ErrorBanner, formatDateTime, Spinner, Table, useApi, useClient, UserText, type Column, type Schema } from '@local-server/shared';

type AccessKey = Schema<'AccessKey'>;

const keyTone = { active: 'good', expired: 'neutral', revoked: 'bad' } as const;
const keyStatus: Record<AccessKey['status'], string> = { active: 'Active', expired: 'Expired', revoked: 'Revoked' };

/** The visible part of a key, as its secret starts (`ls_<prefix>_…`). */
export function keyLabel(k: Pick<AccessKey, 'prefix'>): string {
  return `ls_${k.prefix}_…`;
}

/** Where a key may be used: any device, or only the tailnet device the host bound it to. */
function BoundNode({ k }: { k: AccessKey }) {
  if (!k.bound_node) return <>Any device</>;
  // The machine name is the host's own label for the device; shown as text.
  return (
    <span>
      Only from <UserText text={k.bound_node_name || k.bound_node} className="font-medium" />
    </span>
  );
}

const columns: Column<AccessKey>[] = [
  { key: 'name', header: 'Label', render: (k) => (k.name ? <UserText text={k.name} /> : <span className="italic text-slate-600">No label</span>) },
  { key: 'prefix', header: 'Key starts with', render: (k) => <code className="text-xs">{keyLabel(k)}</code> },
  { key: 'status', header: 'Status', render: (k) => <Badge tone={keyTone[k.status]}>{keyStatus[k.status]}</Badge> },
  { key: 'created', header: 'Issued', render: (k) => formatDateTime(k.created_at) },
  { key: 'expires', header: 'Expires', render: (k) => (k.expires_at ? formatDateTime(k.expires_at) : 'Never') },
  { key: 'node', header: 'Device', render: (k) => <BoundNode k={k} /> },
];

/**
 * My keys: the consumer's access keys by prefix (GET /v1/me/access-keys), read only. The host
 * issues and revokes keys and shows a key's secret once, when it is issued; this page never has it.
 */
export default function KeysPage() {
  const client = useClient();
  const keys = useApi(async () => (await call(client.GET('/v1/me/access-keys'))).data, [client]);
  return (
    <>
      <h1 className="text-xl font-semibold">My keys</h1>
      <p className="text-sm text-slate-700">
        An app calls the host with one of these keys. The host issues and revokes them and shows a key&apos;s secret only once, when it issues it. To get a
        new key, ask the host.
      </p>
      <Card title="Access keys" actions={<Button onClick={() => void keys.refetch()}>Refresh</Button>}>
        <ErrorBanner error={keys.error} title="Your keys could not be read" />
        {keys.loading && !keys.data && <Spinner label="Loading your keys" />}
        {keys.data && <Table caption="Your access keys" columns={columns} rows={keys.data} rowKey={(k) => k.id} empty="You have no access keys yet." />}
      </Card>
    </>
  );
}
