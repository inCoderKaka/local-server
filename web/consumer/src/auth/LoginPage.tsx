import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, Link, useClient, type ApiError } from '@local-server/shared';
import type { Consumer } from '../lib/session';
import { getPasskey, PasskeyError, passkeysSupported } from '../lib/webauthn';

/** The title of a refusal: a lockout says so; the host's message says for how long. */
function titleOf(problem: unknown): string | undefined {
  return (problem as ApiError | null)?.code === 'rate_limit_exceeded' ? 'Logging in is locked for now' : undefined;
}

/**
 * The consumer login (req_001 D-13): a login name and password (POST /v1/session/password), or a
 * passkey this browser holds for the host (a discoverable passkey, so no name is asked). Repeated
 * failures lock logging in for the host's lockout settings; the host's message says how long.
 */
export function LoginPage({ onSignedIn, notice }: { onSignedIn: (c: Consumer) => void; notice?: string }) {
  const client = useClient();
  const canPasskey = passkeysSupported();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [problem, setProblem] = useState<unknown>(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      onSignedIn(await call(client.POST('/v1/session/password', { body: { username, password } })));
    } catch (err) {
      setProblem(asApiError(err));
      setPassword('');
    } finally {
      setBusy(false);
    }
  }

  async function withPasskey() {
    setBusy(true);
    setProblem(null);
    try {
      const ceremony = await call(client.POST('/v1/session/passkey/begin'));
      setStatus("Follow your browser's prompt to use your passkey.");
      const credential = await getPasskey(ceremony.options);
      onSignedIn(await call(client.POST('/v1/session/passkey/finish', { body: { ceremony_id: ceremony.ceremony_id, credential } })));
    } catch (err) {
      setProblem(err instanceof PasskeyError ? err : asApiError(err));
    } finally {
      setStatus('');
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto mt-16 max-w-sm px-4">
      <Card title="Log in">
        <div className="flex flex-col gap-4">
          {notice && (
            <p role="status" className="text-sm text-slate-700">
              {notice}
            </p>
          )}
          <ErrorBanner error={problem} title={titleOf(problem)} />
          <form onSubmit={submit} className="flex flex-col gap-3" aria-label="Log in with a password">
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
          <div className="flex flex-col gap-2 border-t border-slate-200 pt-3">
            {canPasskey ? (
              <Button onClick={() => void withPasskey()} disabled={busy}>
                Log in with a passkey
              </Button>
            ) : (
              <p className="text-sm text-slate-600">This browser cannot use passkeys. Log in with your password.</p>
            )}
            <p role="status" className="text-sm text-slate-700">
              {status}
            </p>
          </div>
          <p className="text-sm">
            Have an invite? <Link to="/signup">Sign up</Link>
          </p>
        </div>
      </Card>
    </div>
  );
}
