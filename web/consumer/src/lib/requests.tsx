import { Badge, call, formatCount, readAll, UserText, type ApiClient, type Schema } from '@local-server/shared';
import { quotaText } from './limits';

export type ResourceRequest = Schema<'ResourceRequest'>;
export type ResourceRequestAsk = Schema<'ResourceRequestAsk'>;
export type ResourceRequestStatus = Schema<'ResourceRequestStatus'>;
type Principal = Schema<'PrincipalRef'>;

/** The route of the requests page, and of one request. */
export const REQUESTS_PATH = '/requests';
export const requestHref = (id: string) => `${REQUESTS_PATH}/${encodeURIComponent(id)}`;

/** How each state reads; `open` reads "Pending", the word the host's admin UI uses too. */
export const statusText: Record<ResourceRequestStatus, string> = {
  open: 'Pending',
  approved: 'Approved',
  rejected: 'Rejected',
  withdrawn: 'Withdrawn',
  expired: 'Expired',
};

const statusTone = { open: 'warn', approved: 'good', rejected: 'bad', withdrawn: 'neutral', expired: 'neutral' } as const;

export function RequestStatusBadge({ status }: { status: ResourceRequestStatus }) {
  return <Badge tone={statusTone[status]}>{statusText[status]}</Badge>;
}

const authorKind: Record<Principal['kind'], string> = { host_admin: 'Host', consumer: 'You', platform_admin: 'Platform admin' };

/** Who wrote a comment: "You", or the host with its name as text. */
export function Author({ author }: { author: Principal }) {
  const kind = authorKind[author.kind] ?? author.kind;
  if (author.kind === 'consumer' || !author.display_name) return <span className="font-medium">{kind}</span>;
  return (
    <span>
      <span className="font-medium">{kind}</span>
      <span className="text-xs text-slate-600">
        {' · '}
        <UserText text={author.display_name} />
      </span>
    </span>
  );
}

/**
 * What a request asks for, one line per field. Model names and request types are the consumer's
 * own text, so every line is shown as text, never HTML (req_001 D-31). The deprecated `quota` is
 * the allowance per the host's default quota window.
 */
export function askLines(ask: ResourceRequestAsk | undefined): string[] {
  if (!ask) return [];
  const out: string[] = [];
  if (ask.models && ask.models.length > 0) out.push(`Models: ${ask.models.join(', ')}`);
  if (ask.context_limit !== undefined) out.push(`Context limit: ${formatCount(ask.context_limit)} tokens`);
  if (ask.max_output_tokens !== undefined) out.push(`Most output tokens per call: ${formatCount(ask.max_output_tokens)}`);
  if (ask.quotas && ask.quotas.length > 0) out.push(`Quotas: ${ask.quotas.map(quotaText).join('; ')}`);
  else if (ask.quota) {
    const parts = [];
    if (ask.quota.tokens !== undefined) parts.push(`${formatCount(ask.quota.tokens)} tokens`);
    if (ask.quota.requests !== undefined) parts.push(`${formatCount(ask.quota.requests)} requests`);
    if (parts.length > 0) out.push(`Quota: ${parts.join(' and ')} per the host's quota window`);
  }
  if (ask.concurrency !== undefined) out.push(`Requests at once: ${formatCount(ask.concurrency)}`);
  if (ask.request_properties && ask.request_properties.length > 0) out.push(`Request types: ${ask.request_properties.join(', ')}`);
  return out;
}

export function AskSummary({ ask, empty }: { ask: ResourceRequestAsk | undefined; empty: string }) {
  const lines = askLines(ask);
  if (lines.length === 0) return <span className="text-slate-600">{empty}</span>;
  return (
    <ul className="flex flex-col gap-0.5">
      {lines.map((l) => (
        <li key={l}>
          <UserText text={l} />
        </li>
      ))}
    </ul>
  );
}

/** The number of the consumer's open requests, every page read (GET /v1/resource-requests?status=open). */
export async function countOpenRequests(client: ApiClient): Promise<number> {
  const open = await readAll<ResourceRequest>((cursor) =>
    call(client.GET('/v1/resource-requests', { params: { query: cursor ? { status: 'open', cursor } : { status: 'open' } } })),
  );
  return open.length;
}
