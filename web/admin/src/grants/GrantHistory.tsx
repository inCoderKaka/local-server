import { Button, call, Card, ErrorBanner, formatDateTime, Spinner, Table, useClient, usePagedList, UserText, type Column } from '@local-server/shared';
import { limitText, overflowText, quotaText, RequestTypes, type GrantVersion } from './labels';

function modelsText(v: GrantVersion): string {
  if (v.limits.models.length === 0) return 'None';
  return v.limits.models
    .map((m) => `${m.model} (context ${limitText(m.context_limit).toLowerCase()}, output ${limitText(m.max_output_tokens).toLowerCase()})`)
    .join('; ');
}

const columns: Column<GrantVersion>[] = [
  { key: 'version', header: 'Version', render: (v) => String(v.version) },
  { key: 'set', header: 'Set', render: (v) => formatDateTime(v.created_at) },
  { key: 'by', header: 'Made by', render: (v) => (v.resource_request_id ? 'Approving a resource request' : 'The host') },
  { key: 'models', header: 'Models', render: (v) => <UserText text={modelsText(v)} /> },
  { key: 'quotas', header: 'Quotas', render: (v) => ((v.limits.quotas ?? []).length ? (v.limits.quotas ?? []).map(quotaText).join('; ') : 'None') },
  { key: 'concurrency', header: 'At once', render: (v) => String(v.limits.concurrency) },
  { key: 'overflow', header: 'Too-long prompt', render: (v) => overflowText[v.limits.on_context_overflow].label },
  { key: 'types', header: 'Request types', render: (v) => <RequestTypes allowed={v.limits.allowed_request_properties} /> },
  { key: 'expires', header: 'Expires', render: (v) => (v.limits.expires_at ? formatDateTime(v.limits.expires_at) : 'Never') },
];

/** Every grant version of a consumer, newest first, a page at a time. `reloadKey` reads it again. */
export function GrantHistory({ consumerId, reloadKey }: { consumerId: string; reloadKey: string }) {
  const client = useClient();
  const list = usePagedList(
    (cursor) => call(client.GET('/admin/v1/consumers/{consumer_id}/grant/versions', { params: { path: { consumer_id: consumerId }, query: { cursor } } })),
    [client, consumerId, reloadKey],
  );
  return (
    <Card title="Grant history">
      <div className="flex flex-col gap-3">
        <ErrorBanner error={list.error} title="The grant history could not be read" />
        {!list.loaded && list.loading && <Spinner label="Loading the grant history" />}
        {list.loaded && <Table caption="Grant versions, newest first" columns={columns} rows={list.items} rowKey={(v) => v.id} empty="No grant versions yet." />}
        {list.next && (
          <div>
            <Button disabled={list.loading} onClick={() => void list.loadMore()}>
              Show more
            </Button>
          </div>
        )}
      </div>
    </Card>
  );
}
