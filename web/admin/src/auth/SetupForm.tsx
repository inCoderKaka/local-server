import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, useClient, type ApiError } from '@local-server/shared';
import type { AdminUser } from '../lib/session';

/** First run: creates the one host admin (POST /admin/v1/setup), which also starts its session. */
export function SetupForm({ onCreated }: { onCreated: (u: AdminUser) => void }) {
  const client = useClient();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [error, setError] = useState<ApiError | null>(null);
  const [mismatch, setMismatch] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (password !== repeat) {
      setMismatch(true);
      return;
    }
    setMismatch(false);
    setBusy(true);
    setError(null);
    try {
      onCreated(await call(client.POST('/admin/v1/setup', { body: { username, password } })));
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto mt-16 max-w-sm">
      <Card title="Create the first admin">
        <form onSubmit={submit} className="flex flex-col gap-3" aria-label="Create the first admin">
          <p className="text-sm text-slate-700">
            No admin exists on this host yet. The login you create here manages the host; there is exactly one.
          </p>
          <ErrorBanner error={error} />
          <Field label="Login name" autoComplete="username" required value={username} onChange={(e) => setUsername(e.target.value)} />
          <Field
            label="Password"
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Field
            label="Repeat the password"
            type="password"
            autoComplete="new-password"
            required
            value={repeat}
            onChange={(e) => setRepeat(e.target.value)}
            error={mismatch ? 'The two passwords differ.' : undefined}
          />
          <Button type="submit" variant="primary" disabled={busy}>
            Create admin
          </Button>
        </form>
      </Card>
    </div>
  );
}
