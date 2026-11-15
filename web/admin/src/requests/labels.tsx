import { useId, type ReactNode, type TextareaHTMLAttributes } from 'react';
import { Badge, UserText, type Schema } from '@local-server/shared';
import { quotaText } from '../grants/labels';
import { askQuotas, type ResourceRequestAsk, type ResourceRequestStatus } from './ask';

type Principal = Schema<'PrincipalRef'>;

/** How each state reads. `open` reads "Pending", the word the shell's count uses. */
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

const kindLabel: Record<Principal['kind'], string> = { host_admin: 'Host admin', consumer: 'Consumer', platform_admin: 'Platform admin' };

/** Who wrote a comment: the name, as text, and the kind of principal. */
export function Author({ author }: { author: Principal }) {
  return (
    <span>
      {author.display_name ? <UserText className="font-medium" text={author.display_name} /> : null}
      <span className="text-xs text-slate-600">
        {author.display_name ? ' · ' : ''}
        {kindLabel[author.kind] ?? author.kind}
      </span>
    </span>
  );
}

/**
 * What a request asks for, one line per field. Model names and request types come from the
 * consumer, so every line is text, never HTML (req_001 D-31). The deprecated `quota` is only
 * shown when the host's quota window is known, as the entries it stands for.
 */
export function askLines(ask: ResourceRequestAsk | undefined, quotaWindowSeconds: number | null): string[] {
  if (!ask) return [];
  const out: string[] = [];
  if (ask.models && ask.models.length > 0) out.push(`Models: ${ask.models.join(', ')}`);
  if (ask.context_limit !== undefined) out.push(`Context limit: ${ask.context_limit} tokens`);
  if (ask.max_output_tokens !== undefined) out.push(`Most output tokens per call: ${ask.max_output_tokens}`);
  const quotas = askQuotas(ask, quotaWindowSeconds);
  if (quotas.length > 0) out.push(`Quotas: ${quotas.map(quotaText).join('; ')}`);
  if (ask.concurrency !== undefined) out.push(`Requests at once: ${ask.concurrency}`);
  if (ask.request_properties && ask.request_properties.length > 0) out.push(`Request types: ${ask.request_properties.join(', ')}`);
  return out;
}

export function AskSummary({ ask, quotaWindowSeconds, empty }: { ask: ResourceRequestAsk | undefined; quotaWindowSeconds: number | null; empty: ReactNode }) {
  const lines = askLines(ask, quotaWindowSeconds);
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

/** A labelled multi-line text field with an optional hint. */
export function TextArea({ label, hint, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement> & { label: ReactNode; hint?: ReactNode }) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium text-slate-800">
        {label}
      </label>
      <textarea id={id} rows={3} aria-describedby={hintId} className="rounded border border-slate-300 px-2 py-1 text-sm" {...rest} />
      {hint && (
        <p id={hintId} className="text-xs text-slate-600">
          {hint}
        </p>
      )}
    </div>
  );
}
