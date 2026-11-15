import type { Schema } from '@local-server/shared';
import type { GrantLimits, GrantModelLimit, QuotaEntry } from '../grants/labels';

export type ResourceRequest = Schema<'ResourceRequest'>;
export type ResourceRequestAsk = Schema<'ResourceRequestAsk'>;
export type ResourceRequestStatus = Schema<'ResourceRequestStatus'>;

/** The quota entries an ask names: its `quotas`, else its deprecated `quota` at the host's quota window. */
export function askQuotas(ask: ResourceRequestAsk, quotaWindowSeconds: number | null): QuotaEntry[] {
  if (ask.quotas) return ask.quotas;
  const q = ask.quota;
  if (!q || quotaWindowSeconds === null) return [];
  const out: QuotaEntry[] = [];
  if (q.tokens !== undefined) out.push({ metric: 'tokens', limit: q.tokens, window_seconds: quotaWindowSeconds });
  if (q.requests !== undefined) out.push({ metric: 'requests', limit: q.requests, window_seconds: quotaWindowSeconds });
  return out;
}

/**
 * The limits an approval form starts from: the consumer's current grant (or a new, empty one)
 * with what the request asks for applied on top (req_001 D-39). An approval replaces the whole
 * grant, so the current terms are kept and only the asked fields change:
 * - asked models are added; the asked context and output limits apply to the asked models, or to
 *   every model of the grant when the ask names none;
 * - an asked quota replaces the entry with the same metric and window, or is added;
 * - an asked concurrency replaces the grant's;
 * - asked request types are added when the grant allows only some.
 * The host edits the result before approving.
 */
export function limitsFromAsk(
  current: GrantLimits | null,
  ask: ResourceRequestAsk | undefined,
  opts: { defaultConcurrency: number | null; quotaWindowSeconds: number | null },
): GrantLimits {
  const a = ask ?? {};
  const models: GrantModelLimit[] = (current?.models ?? []).map((m) => ({ ...m }));
  for (const name of a.models ?? []) {
    if (!models.some((m) => m.model === name)) models.push({ model: name, context_limit: null, max_output_tokens: null });
  }
  const targets = a.models && a.models.length > 0 ? new Set(a.models) : null;
  for (const m of models) {
    if (targets && !targets.has(m.model)) continue;
    if (a.context_limit !== undefined) m.context_limit = a.context_limit;
    if (a.max_output_tokens !== undefined) m.max_output_tokens = a.max_output_tokens;
  }

  const quotas: QuotaEntry[] = (current?.quotas ?? []).map((q) => ({ ...q }));
  for (const q of askQuotas(a, opts.quotaWindowSeconds)) {
    const same = quotas.find((x) => x.metric === q.metric && x.window_seconds === q.window_seconds);
    if (same) same.limit = q.limit;
    else quotas.push({ ...q });
  }

  let allowed = current ? (current.allowed_request_properties ?? null) : null;
  if (allowed !== null && a.request_properties) allowed = [...new Set([...allowed, ...a.request_properties])];

  return {
    models,
    quotas,
    // 0 when nothing gives one: the form then starts the field empty and asks for it.
    concurrency: a.concurrency ?? current?.concurrency ?? opts.defaultConcurrency ?? 0,
    on_context_overflow: current?.on_context_overflow ?? 'refuse',
    allowed_request_properties: allowed,
    expires_at: current?.expires_at ?? null,
  };
}
