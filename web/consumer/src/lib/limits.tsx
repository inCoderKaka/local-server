import { asApiError, call, formatCount, formatSeconds, UserText, useApi, useClient, type ApiState, type Schema } from '@local-server/shared';

export type GrantVersion = Schema<'GrantVersion'>;
export type GrantLimits = Schema<'GrantLimits'>;
export type QuotaEntry = Schema<'QuotaEntry'>;
type ContextOverflow = Schema<'ContextOverflow'>;
type BillingMode = Schema<'BillingMode'>;

/**
 * The consumer's current grant version (GET /v1/me/limits), or null when the host has not given
 * this account any limits yet: the host answers 404 then, which is "no access yet" (req_001
 * D-150), not an error.
 */
export function useMyLimits(): ApiState<GrantVersion | null> {
  const client = useClient();
  return useApi(async () => {
    try {
      return await call(client.GET('/v1/me/limits'));
    } catch (e) {
      if (asApiError(e).status === 404) return null;
      throw e;
    }
  }, [client]);
}

/** A grant limit for the reader: the number, or the host's default when the grant sets none. */
export function limitText(v: number | null | undefined, unit: string): string {
  return v === null || v === undefined ? "The host's default" : `${formatCount(v)} ${unit}`;
}

const metricWord: Record<QuotaEntry['metric'], string> = { tokens: 'tokens', requests: 'requests' };

/** One quota entry in words: `100,000 tokens every 24h`. */
export function quotaText(q: QuotaEntry): string {
  return `${formatCount(q.limit)} ${metricWord[q.metric]} every ${formatSeconds(q.window_seconds)}`;
}

export const metricLabel: Record<QuotaEntry['metric'], string> = { tokens: 'Tokens', requests: 'Requests' };

/** What happens to a prompt longer than the context limit, said to the consumer (req_001 D-48). */
export const overflowText: Record<ContextOverflow, string> = {
  refuse: 'It is refused with the error context_too_long, and nothing is cut. Shorten it and try again.',
  truncate: 'It is cut to fit: the oldest messages are dropped and the last one is kept, so the answer may miss what was dropped.',
};

export const billingText: Record<BillingMode, string> = {
  none: 'No billing. The host sets the quota windows of your keys.',
  prepaid: 'Prepaid. You may set quota windows on your own keys. This version keeps no balances or invoices.',
  postpaid: 'Postpaid. You may set quota windows on your own keys. This version keeps no balances or invoices.',
};

/**
 * The request types the grant allows. Their names can come from a request the consumer wrote, so
 * they are shown as text only.
 */
export function RequestTypes({ allowed }: { allowed: string[] | null | undefined }) {
  if (allowed === null || allowed === undefined) return <>Any request type</>;
  if (allowed.length === 0) return <>None: a call that names a request type is refused</>;
  return <UserText text={allowed.join(', ')} />;
}

/** The limits in one line, for the start page: the models and how many requests at once. */
export function limitsLine(l: GrantLimits): string {
  if (l.models.length === 0) return 'Your grant allows no model yet, so every call is refused.';
  const names = l.models.map((m) => m.model).join(', ');
  const models = l.models.length === 1 ? `1 model (${names})` : `${l.models.length} models (${names})`;
  const at = l.concurrency === 1 ? '1 request at once' : `${formatCount(l.concurrency)} requests at once`;
  return `You may use ${models}, ${at}.`;
}
