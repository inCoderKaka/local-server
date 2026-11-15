import { formatSeconds, UserText, type Schema } from '@local-server/shared';

export type GrantVersion = Schema<'GrantVersion'>;
export type GrantLimits = Schema<'GrantLimits'>;
export type GrantModelLimit = Schema<'GrantModelLimit'>;
export type QuotaEntry = Schema<'QuotaEntry'>;
export type ContextOverflow = Schema<'ContextOverflow'>;

/** What each context overflow choice does, in plain words (req_001 D-48). */
export const overflowText: Record<ContextOverflow, { label: string; help: string }> = {
  refuse: {
    label: 'Refuse the call',
    help: 'A prompt longer than the context limit is refused with the error context_too_long. Nothing is cut, so the consumer knows to shorten it.',
  },
  truncate: {
    label: 'Cut the prompt to fit',
    help: 'The engine drops the oldest messages until the prompt fits and keeps the last one. The answer may miss what was dropped.',
  },
};

export const metricText: Record<QuotaEntry['metric'], string> = { tokens: 'Tokens', requests: 'Requests' };

/** A grant limit: a number, or "No limit" when the grant sets none (req_001 D-157). */
export function limitText(v: number | null | undefined): string {
  return v === null || v === undefined ? 'No limit' : String(v);
}

/** One quota entry in words: `100000 tokens every 24h`. */
export function quotaText(q: QuotaEntry): string {
  return `${q.limit} ${q.metric} every ${formatSeconds(q.window_seconds)}`;
}

/**
 * The request types (request properties) a grant allows. Their names can come from a resource
 * request a consumer wrote, so they are shown as text only.
 */
export function RequestTypes({ allowed }: { allowed: string[] | null | undefined }) {
  if (allowed === null || allowed === undefined) return <>Any request type</>;
  if (allowed.length === 0) return <>None: a call that names a request type is refused</>;
  return <UserText text={allowed.join(', ')} />;
}
