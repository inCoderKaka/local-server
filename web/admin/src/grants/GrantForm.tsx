import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { asApiError, Button, call, CheckboxField, ErrorBanner, Field, formatSeconds, Link, listFrom, secondsFrom, useClient, UserText, wholeFrom, type ApiError, type Schema } from '@local-server/shared';
import { ExpiryChoice, expiryValue, noExpiry, type Expiry } from '../consumers/ExpiryChoice';
import { useSettings } from '../lib/settings';
import { metricText, overflowText, type ContextOverflow, type GrantLimits, type GrantVersion, type QuotaEntry } from './labels';
import { HOST_SETTING, intSetting, localInputOf, textSetting } from './values';

type HostModel = Schema<'HostModel'>;

interface ModelChoice {
  on: boolean;
  context: string;
  output: string;
}

interface QuotaRow {
  /** A stable key for React; rows are added and removed. */
  key: number;
  metric: QuotaEntry['metric'];
  limit: string;
  window: string;
}

interface Values {
  models: Record<string, ModelChoice>;
  quotas: QuotaRow[];
  concurrency: string;
  overflow: ContextOverflow;
  typesMode: 'any' | 'only';
  types: string;
  expiry: Expiry;
}

let rowKey = 0;

/** The choice for a model name, read only from the object's own entries so a name like __proto__ finds nothing. */
function modelOf(models: Record<string, ModelChoice>, name: string): ModelChoice | undefined {
  return Object.hasOwn(models, name) ? models[name] : undefined;
}

function valuesOf(l: GrantLimits | null, defaultConcurrency: number | null): Values {
  if (!l) {
    return {
      models: {},
      quotas: [],
      // A new grant starts from the host's own default concurrency, when the setting is readable.
      concurrency: defaultConcurrency !== null ? String(defaultConcurrency) : '',
      overflow: 'refuse',
      typesMode: 'any',
      types: '',
      expiry: noExpiry,
    };
  }
  // Object.fromEntries makes ordinary entries: a model named __proto__ never reaches the prototype.
  const models: Record<string, ModelChoice> = Object.fromEntries(
    l.models.map((m) => [m.model, { on: true, context: m.context_limit == null ? '' : String(m.context_limit), output: m.max_output_tokens == null ? '' : String(m.max_output_tokens) }]),
  );
  return {
    models,
    quotas: (l.quotas ?? []).map((q) => ({ key: ++rowKey, metric: q.metric, limit: String(q.limit), window: formatSeconds(q.window_seconds) })),
    // Below 1 is no concurrency yet (an approval with none to start from): the field starts empty.
    concurrency: l.concurrency >= 1 ? String(l.concurrency) : '',
    overflow: l.on_context_overflow,
    typesMode: l.allowed_request_properties == null ? 'any' : 'only',
    types: (l.allowed_request_properties ?? []).join(', '),
    expiry: l.expires_at ? { mode: 'at', at: localInputOf(l.expires_at) } : noExpiry,
  };
}

/** Matches a refused field: the host names grant fields `limits.<field>`, a decoding error `<field>`. */
function paramIs(error: ApiError | null, field: string): boolean {
  return !!error && (error.param === `limits.${field}` || error.param === field);
}

/**
 * Sets a consumer's grant: its models with a context limit and an output limit each, its quotas,
 * how many calls may run at once, what a too-long prompt does, the request types it may send and
 * when it expires (req_001 D-25, D-48, D-107). Each save stores a new grant version; empty limits
 * are sent as null, which means the grant sets none and the host's own setting applies.
 *
 * The requests page uses the same form to approve a resource request: it passes the limits to
 * start from and `send`, which posts the approval instead of PUT .../grant (req_001 D-39).
 */
export function GrantForm({
  consumerId,
  current,
  hostModels,
  onSaved,
  start,
  send,
  label = 'Set the grant',
  submitLabel = 'Save grant',
  children,
}: {
  consumerId: string;
  current: GrantVersion | null;
  /** The host's models, or undefined while they are not read. */
  hostModels: HostModel[] | undefined;
  /** Called with the stored version after a PUT; not called when `send` is given. */
  onSaved?: (v: GrantVersion) => void;
  /** The limits the form starts from instead of the current grant's. */
  start?: GrantLimits;
  /** Sends the limits instead of PUT .../grant; an error it throws is shown like a refused save. */
  send?: (limits: GrantLimits) => Promise<void>;
  /** The form's accessible name. */
  label?: string;
  submitLabel?: string;
  /** More fields, shown above the submit button. */
  children?: ReactNode;
}) {
  const startLimits = start ?? current?.limits ?? null;
  const client = useClient();
  const settings = useSettings();
  const overflowName = useId();
  const typesName = useId();
  const [form, setForm] = useState<Values>(() => valuesOf(startLimits, intSetting(settings.list, HOST_SETTING.grantConcurrency)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [problems, setProblems] = useState<Record<string, string>>({});
  // The model names in the order the last request sent them: a refusal names a model by index.
  const [sent, setSent] = useState<string[]>([]);
  const concurrencyTouched = useRef(false);

  // The settings can arrive after the form opened; a new grant then takes the default once.
  const defaultConcurrency = intSetting(settings.list, HOST_SETTING.grantConcurrency);
  useEffect(() => {
    if (!startLimits && defaultConcurrency !== null && !concurrencyTouched.current) {
      setForm((f) => (f.concurrency === '' ? { ...f, concurrency: String(defaultConcurrency) } : f));
    }
  }, [startLimits, defaultConcurrency]);

  const set = (patch: Partial<Values>) => {
    if ('concurrency' in patch) concurrencyTouched.current = true;
    setForm((f) => ({ ...f, ...patch }));
  };
  const setModel = (name: string, patch: Partial<ModelChoice>) =>
    setForm((f) => ({ ...f, models: { ...f.models, [name]: { on: false, context: '', output: '', ...modelOf(f.models, name), ...patch } } }));
  const setQuota = (key: number, patch: Partial<QuotaRow>) => setForm((f) => ({ ...f, quotas: f.quotas.map((q) => (q.key === key ? { ...q, ...patch } : q)) }));

  // The models offered: every model the host shares, and every model this grant already has.
  const shared = new Set((hostModels ?? []).filter((m) => m.allowed).map((m) => m.name));
  const names = [...new Set([...shared, ...Object.keys(form.models)])].sort((a, b) => a.localeCompare(b));

  const contextDefault = intSetting(settings.list, HOST_SETTING.contextTokens);
  const outputDefault = intSetting(settings.list, HOST_SETTING.maxTokensCeiling);
  const windowDefault = textSetting(settings.list, HOST_SETTING.quotaWindow);
  const windowMin = textSetting(settings.list, HOST_SETTING.quotaWindowMin);
  const windowMax = textSetting(settings.list, HOST_SETTING.quotaWindowMax);

  function serverError(field: string): string | undefined {
    return paramIs(error, field) ? error!.message : undefined;
  }
  function modelError(name: string, field: 'model' | 'context_limit' | 'max_output_tokens'): string | undefined {
    const i = sent.indexOf(name);
    return problems[`models.${name}.${field}`] ?? (i >= 0 ? serverError(`models.${i}.${field}`) : undefined);
  }
  function quotaError(index: number, field: 'metric' | 'limit' | 'window_seconds'): string | undefined {
    return problems[`quotas.${index}.${field}`] ?? serverError(`quotas.${index}.${field}`);
  }

  /** The request body, or the problems that keep it from being sent. */
  function build(): { body?: GrantLimits; problems: Record<string, string> } {
    const p: Record<string, string> = {};
    const models: GrantLimits['models'] = [];
    for (const name of names) {
      const m = modelOf(form.models, name);
      if (!m?.on) continue;
      const ctx = wholeFrom(m.context, 1);
      const out = wholeFrom(m.output, 1);
      if (ctx.error) p[`models.${name}.context_limit`] = ctx.error;
      if (out.error) p[`models.${name}.max_output_tokens`] = out.error;
      models.push({ model: name, context_limit: ctx.value ?? null, max_output_tokens: out.value ?? null });
    }
    const quotas: QuotaEntry[] = [];
    form.quotas.forEach((q, i) => {
      const limit = wholeFrom(q.limit, 0);
      if (limit.error || limit.value === undefined) p[`quotas.${i}.limit`] = limit.error ?? 'Write how many are allowed; 0 allows none.';
      const w = secondsFrom(q.window);
      if (w.error || !w.value) p[`quotas.${i}.window_seconds`] = w.error ?? 'The window must be longer than 0.';
      quotas.push({ metric: q.metric, limit: limit.value ?? 0, window_seconds: w.value ?? 0 });
    });
    const conc = wholeFrom(form.concurrency, 1);
    if (conc.error || conc.value === undefined) p.concurrency = conc.error ?? 'Write how many calls may run at once, 1 or more.';
    const exp = expiryValue(form.expiry);
    if (exp.error) p.expires_at = exp.error;
    if (Object.keys(p).length > 0) return { problems: p };
    return {
      problems: p,
      body: {
        models,
        quotas,
        concurrency: conc.value!,
        on_context_overflow: form.overflow,
        allowed_request_properties: form.typesMode === 'any' ? null : listFrom(form.types),
        expires_at: exp.value ?? null,
      },
    };
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    const { body, problems: p } = build();
    setProblems(p);
    if (!body) return;
    setBusy(true);
    setError(null);
    setSent(body.models.map((m) => m.model));
    try {
      if (send) await send(body);
      else onSaved?.(await call(client.PUT('/admin/v1/consumers/{consumer_id}/grant', { params: { path: { consumer_id: consumerId } }, body })));
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} noValidate aria-label={label} className="flex flex-col gap-4">
      <p className="text-sm text-slate-700">
        Saving stores a new grant version and applies it to the consumer's next call. Earlier versions stay in the history below, and usage
        records keep the version they ran under.
      </p>
      <ErrorBanner error={error} title="The grant was not saved" />

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">Models</legend>
        <p className="text-xs text-slate-600">
          Only models you share are offered; share more on the{' '}
          <Link to="/models" className="text-sky-800 underline">
            Models
          </Link>{' '}
          page. Leave a limit empty for no limit: the host then uses its own setting
          {contextDefault !== null && outputDefault !== null
            ? ` (now a context of ${contextDefault} tokens and at most ${outputDefault} output tokens)`
            : ''}
          .
        </p>
        {serverError('models') && <p className="text-xs text-red-700">{serverError('models')}</p>}
        {hostModels === undefined && names.length === 0 && <p className="text-sm text-slate-600">Reading the host's models…</p>}
        {hostModels !== undefined && names.length === 0 && <p className="text-sm text-slate-600">The host shares no models yet.</p>}
        {names.map((name) => {
          const m = modelOf(form.models, name) ?? { on: false, context: '', output: '' };
          const notShared = hostModels !== undefined && !shared.has(name);
          return (
            <div key={name} className="flex flex-col gap-2 rounded border border-slate-200 p-2">
              <CheckboxField label={<UserText text={name} />} aria-label={`Allow ${name}`} checked={m.on} onChange={(e) => setModel(name, { on: e.target.checked })} />
              {notShared && m.on && (
                <p className="text-xs text-amber-900">This model is not shared now: the host refuses a new version that includes it until you share it again.</p>
              )}
              {modelError(name, 'model') && <p className="text-xs text-red-700">{modelError(name, 'model')}</p>}
              {m.on && (
                <div className="grid gap-2 sm:grid-cols-2">
                  <Field
                    label={`Context limit for ${name} (tokens)`}
                    inputMode="numeric"
                    placeholder="No limit"
                    value={m.context}
                    onChange={(e) => setModel(name, { context: e.target.value })}
                    error={modelError(name, 'context_limit')}
                  />
                  <Field
                    label={`Most output tokens per call for ${name}`}
                    inputMode="numeric"
                    placeholder="No limit"
                    value={m.output}
                    onChange={(e) => setModel(name, { output: e.target.value })}
                    error={modelError(name, 'max_output_tokens')}
                  />
                </div>
              )}
            </div>
          );
        })}
      </fieldset>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">Quotas</legend>
        <p className="text-xs text-slate-600">
          Each quota caps tokens or requests in a repeating window, for example 100000 tokens every 24h. No quota means use is not capped by one.
          {windowMin && windowMax ? ` A window is between ${windowMin} and ${windowMax} (host settings).` : ''}
        </p>
        {serverError('quotas') && <p className="text-xs text-red-700">{serverError('quotas')}</p>}
        {form.quotas.map((q, i) => (
          <div key={q.key} role="group" aria-label={`Quota ${i + 1}`} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]">
            <div className="flex flex-col gap-1">
              <label htmlFor={`${overflowName}-q${q.key}`} className="text-sm font-medium text-slate-800">
                Counts
              </label>
              <select
                id={`${overflowName}-q${q.key}`}
                value={q.metric}
                onChange={(e) => setQuota(q.key, { metric: e.target.value as QuotaEntry['metric'] })}
                className="rounded border border-slate-300 px-2 py-1 text-sm"
              >
                {(Object.keys(metricText) as QuotaEntry['metric'][]).map((k) => (
                  <option key={k} value={k}>
                    {metricText[k]}
                  </option>
                ))}
              </select>
              {quotaError(i, 'metric') && <p className="text-xs text-red-700">{quotaError(i, 'metric')}</p>}
            </div>
            <Field label="At most" inputMode="numeric" value={q.limit} onChange={(e) => setQuota(q.key, { limit: e.target.value })} error={quotaError(i, 'limit')} />
            <Field
              label="Every"
              hint="A duration such as 1h or 24h"
              value={q.window}
              onChange={(e) => setQuota(q.key, { window: e.target.value })}
              error={quotaError(i, 'window_seconds')}
            />
            <Button onClick={() => set({ quotas: form.quotas.filter((x) => x.key !== q.key) })} aria-label={`Remove quota ${i + 1}`}>
              Remove
            </Button>
          </div>
        ))}
        <div>
          <Button onClick={() => set({ quotas: [...form.quotas, { key: ++rowKey, metric: 'tokens', limit: '', window: windowDefault ?? '' }] })}>Add a quota</Button>
        </div>
      </fieldset>

      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label="Requests at once"
          hint="How many of this consumer's calls may run at the same time."
          inputMode="numeric"
          value={form.concurrency}
          onChange={(e) => set({ concurrency: e.target.value })}
          error={problems.concurrency ?? serverError('concurrency')}
        />
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">If a prompt is too long</legend>
        <p className="text-xs text-slate-600">What happens when a prompt is longer than the model's context limit.</p>
        {(Object.keys(overflowText) as ContextOverflow[]).map((k) => (
          <label key={k} className="flex items-start gap-2 text-sm">
            <input type="radio" name={overflowName} className="mt-1" checked={form.overflow === k} onChange={() => set({ overflow: k })} />
            <span>
              <span className="font-medium">{overflowText[k].label}.</span> {overflowText[k].help}
            </span>
          </label>
        ))}
        {serverError('on_context_overflow') && <p className="text-xs text-red-700">{serverError('on_context_overflow')}</p>}
      </fieldset>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">Allowed request types</legend>
        <p className="text-xs text-slate-600">
          Request types are the request properties a call names in its LS-Request-Properties header, for example background. Rules use them.
        </p>
        <label className="flex items-center gap-2 text-sm">
          <input type="radio" name={typesName} checked={form.typesMode === 'any'} onChange={() => set({ typesMode: 'any' })} />
          Any request type
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="radio" name={typesName} checked={form.typesMode === 'only'} onChange={() => set({ typesMode: 'only' })} />
          Only these
        </label>
        {form.typesMode === 'only' && (
          <Field
            label="Request types this consumer may send"
            hint="Names separated by commas. Leave it empty to allow none: a call that names one is refused."
            value={form.types}
            onChange={(e) => set({ types: e.target.value })}
            error={serverError('allowed_request_properties')}
          />
        )}
        {form.typesMode === 'any' && serverError('allowed_request_properties') && (
          <p className="text-xs text-red-700">{serverError('allowed_request_properties')}</p>
        )}
      </fieldset>

      <ExpiryChoice value={form.expiry} onChange={(expiry) => set({ expiry })} defaultLabel="Never" error={problems.expires_at ?? serverError('expires_at')} />

      {children}

      <div>
        <Button type="submit" variant="primary" disabled={busy}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
