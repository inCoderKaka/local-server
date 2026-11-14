import { useId, useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, navigate, useClient, type ApiError } from '@local-server/shared';
import { kindLabel, type Consumer } from './labels';

type Kind = Consumer['kind'];

/**
 * Creates a key-only consumer: an account with access keys and no login, for example a client
 * app (req_001 D-25). People usually join through an invite. On success the consumer's page
 * opens, where its first key is issued.
 */
export function CreateConsumerForm() {
  const client = useClient();
  const kindId = useId();
  const [kind, setKind] = useState<Kind | ''>('');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [missing, setMissing] = useState<{ kind?: string; display_name?: string }>({});

  const fieldError = (param: string) => (error?.param === param ? error.message : undefined);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const m: typeof missing = {};
    if (!kind) m.kind = 'Choose a kind.';
    if (!name.trim()) m.display_name = 'Give a display name.';
    setMissing(m);
    if (!kind || m.display_name) return;
    setBusy(true);
    setError(null);
    try {
      const c = await call(
        client.POST('/admin/v1/consumers', {
          body: { kind, display_name: name.trim(), ...(email.trim() ? { email: email.trim() } : {}) },
        }),
      );
      navigate(`/consumers/${encodeURIComponent(c.id)}`);
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  const kindError = missing.kind ?? fieldError('kind');
  return (
    <Card title="New key-only consumer">
      <form onSubmit={submit} noValidate aria-label="New key-only consumer" className="flex flex-col gap-3">
        <p className="text-sm text-slate-700">
          A key-only consumer has access keys but no login, for example a client app. To let a person sign up themselves, create an invite instead.
        </p>
        <ErrorBanner error={error} title="The consumer was not created" />
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="flex flex-col gap-1">
            <label htmlFor={kindId} className="text-sm font-medium text-slate-800">
              Kind
            </label>
            <select
              id={kindId}
              value={kind}
              aria-invalid={kindError ? true : undefined}
              aria-describedby={kindError ? `${kindId}-error` : undefined}
              onChange={(e) => setKind(e.target.value as Kind | '')}
              className="rounded border border-slate-300 px-2 py-1 text-sm"
            >
              <option value="">Choose a kind</option>
              {(Object.keys(kindLabel) as Kind[]).map((k) => (
                <option key={k} value={k}>
                  {kindLabel[k]}
                </option>
              ))}
            </select>
            {kindError && (
              <p id={`${kindId}-error`} className="text-xs text-red-700">
                {kindError}
              </p>
            )}
          </div>
          <Field
            label="Display name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={missing.display_name ?? fieldError('display_name')}
          />
          <Field
            label="Email (optional)"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            error={fieldError('email')}
          />
        </div>
        <div>
          <Button type="submit" variant="primary" disabled={busy}>
            Create consumer
          </Button>
        </div>
      </form>
    </Card>
  );
}
