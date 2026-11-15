import { useState, type FormEvent } from 'react';
import {
  asApiError,
  Button,
  call,
  Card,
  ErrorBanner,
  Field,
  formatSeconds,
  listFrom,
  secondsFrom,
  TextArea,
  useClient,
  wholeFrom,
  type ApiError,
} from '@local-server/shared';
import type { GrantLimits, QuotaEntry } from '../lib/limits';
import type { ResourceRequest, ResourceRequestAsk } from '../lib/requests';

interface Draft {
  message: string;
  models: string;
  context: string;
  output: string;
  concurrency: string;
  tokens: string;
  tokensWindow: string;
  requests: string;
  requestsWindow: string;
  types: string;
}

const empty: Draft = {
  message: '',
  models: '',
  context: '',
  output: '',
  concurrency: '',
  tokens: '',
  tokensWindow: '',
  requests: '',
  requestsWindow: '',
  types: '',
};

type Errors = Partial<Record<keyof Draft, string>>;

/** The window the current grant counts a metric in, to start the window field from (the host's value). */
function windowOf(current: GrantLimits | null | undefined, metric: QuotaEntry['metric']): string {
  const q = current?.quotas?.find((x) => x.metric === metric);
  return q ? formatSeconds(q.window_seconds) : '';
}

/** The ask a draft says, or the errors of its fields. Every field of the ask is optional. */
function askOf(d: Draft): { ask: ResourceRequestAsk; errors: Errors } {
  const errors: Errors = {};
  const ask: ResourceRequestAsk = {};
  const whole = (field: keyof Draft, min: number) => {
    const r = wholeFrom(d[field], min);
    if (r.error) errors[field] = r.error;
    return r.value;
  };
  const models = listFrom(d.models);
  if (models.length > 0) ask.models = models;
  const context = whole('context', 1);
  if (context !== undefined) ask.context_limit = context;
  const output = whole('output', 1);
  if (output !== undefined) ask.max_output_tokens = output;
  const concurrency = whole('concurrency', 1);
  if (concurrency !== undefined) ask.concurrency = concurrency;
  const quotas: QuotaEntry[] = [];
  for (const [metric, field, windowField] of [
    ['tokens', 'tokens', 'tokensWindow'],
    ['requests', 'requests', 'requestsWindow'],
  ] as const) {
    const limit = whole(field, 0);
    if (limit === undefined) continue;
    const w = d[windowField].trim();
    if (w === '') {
      errors[windowField] = 'Write the window this amount is for, such as 24h.';
      continue;
    }
    const s = secondsFrom(w);
    if (s.error || !s.value) errors[windowField] = s.error ?? 'The window must be longer than 0s.';
    else quotas.push({ metric, limit, window_seconds: s.value });
  }
  if (quotas.length > 0) ask.quotas = quotas;
  const types = listFrom(d.types);
  if (types.length > 0) ask.request_properties = types;
  return { ask, errors };
}

/**
 * Asks the host for more (POST /v1/resource-requests): a message, and what is wanted, every field
 * optional. The host decides the resulting limits. A field of the ask the host does not know is
 * ignored and named in the answer's warnings, which `onCreated` gets with the new request.
 */
export function CreateRequestForm({ current, onCreated }: { current: GrantLimits | null | undefined; onCreated: (r: ResourceRequest) => void }) {
  const client = useClient();
  const [d, setD] = useState<Draft>(() => ({ ...empty, tokensWindow: windowOf(current, 'tokens'), requestsWindow: windowOf(current, 'requests') }));
  const [errors, setErrors] = useState<Errors>({});
  const [error, setError] = useState<ApiError | string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof Draft) => (e: { target: { value: string } }) => setD((x) => ({ ...x, [k]: e.target.value }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    const { ask, errors: errs } = askOf(d);
    const noMessage = d.message.trim() === '';
    setErrors(noMessage ? { ...errs, message: 'Say what you need and why.' } : errs);
    if (noMessage || Object.keys(errs).length > 0) {
      setError('Some fields need a fix; see the notes under them.');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const r = await call(client.POST('/v1/resource-requests', { body: { message: d.message, ...(Object.keys(ask).length > 0 ? { ask } : {}) } }));
      setD({ ...empty, tokensWindow: windowOf(current, 'tokens'), requestsWindow: windowOf(current, 'requests') });
      setErrors({});
      onCreated(r);
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Ask the host for more">
      <form onSubmit={submit} noValidate aria-label="New resource request" className="flex flex-col gap-3">
        <ErrorBanner error={error} title={typeof error === 'string' ? undefined : 'The request was not sent'} />
        <TextArea
          label="What you need and why"
          required
          value={d.message}
          onChange={set('message')}
          hint="The host reads this with your request."
          error={errors.message}
        />
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-1 text-sm font-medium text-slate-800">What you ask for (all optional)</legend>
          <Field label="Models" value={d.models} onChange={set('models')} hint="Model names, separated by commas." error={errors.models} />
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="Context limit (tokens)" inputMode="numeric" value={d.context} onChange={set('context')} error={errors.context} />
            <Field label="Most output tokens per call" inputMode="numeric" value={d.output} onChange={set('output')} error={errors.output} />
            <Field label="Requests at once" inputMode="numeric" value={d.concurrency} onChange={set('concurrency')} error={errors.concurrency} />
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Tokens per window" inputMode="numeric" value={d.tokens} onChange={set('tokens')} error={errors.tokens} />
            <Field label="Token window" value={d.tokensWindow} onChange={set('tokensWindow')} hint="Such as 24h." error={errors.tokensWindow} />
            <Field label="Requests per window" inputMode="numeric" value={d.requests} onChange={set('requests')} error={errors.requests} />
            <Field label="Request window" value={d.requestsWindow} onChange={set('requestsWindow')} hint="Such as 24h." error={errors.requestsWindow} />
          </div>
          <p className="text-xs text-slate-600">
            Unless your account is billed, the host sets the quota windows; it decides every limit when it answers.
          </p>
          <Field label="Request types" value={d.types} onChange={set('types')} hint="Request property names, separated by commas." error={errors.types} />
        </fieldset>
        <div>
          <Button type="submit" variant="primary" disabled={busy}>
            Send request
          </Button>
        </div>
      </form>
    </Card>
  );
}
