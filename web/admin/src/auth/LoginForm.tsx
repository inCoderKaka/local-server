import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, useClient, type ApiError } from '@local-server/shared';
import type { AdminUser } from '../lib/session';

/** The admin login (POST /admin/v1/session). Repeated failures lock it out; the server says how. */
export function LoginForm({ onLoggedIn, notice }: { onLoggedIn: (u: AdminUser) => void; notice?: string }) {
  const client = useClient();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onLoggedIn(await call(client.POST('/admin/v1/session', { body: { username, password } })));
    } catch (err) {
      setError(asApiError(err));
      setPassword('');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto mt-16 max-w-sm">
      <Card title="Log in to local-server admin">
        <form onSubmit={submit} className="flex flex-col gap-3" aria-label="Log in">
          {notice && (
            <p role="status" className="text-sm text-slate-700">
              {notice}
            </p>
          )}
          <ErrorBanner
            error={error}
            title={error?.code === 'rate_limit_exceeded' ? 'Logging in is locked for now' : undefined}
          />
          <Field label="Login name" autoComplete="username" required value={username} onChange={(e) => setUsername(e.target.value)} />
          <Field
            label="Password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Button type="submit" variant="primary" disabled={busy}>
            Log in
          </Button>
        </form>
      </Card>
    </div>
  );
}
