import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ApiProvider,
  asApiError,
  Button,
  call,
  createApiClient,
  ErrorBanner,
  isUnauthorized,
  Spinner,
  type ApiClientOptions,
  type ApiError,
} from '@local-server/shared';
import { LoginForm } from './auth/LoginForm';
import { SetupForm } from './auth/SetupForm';
import { Layout } from './layout/Layout';
import { SessionProvider, type AdminUser } from './lib/session';
import { SettingsProvider } from './lib/settings';
import type { PageDef } from './registry';

type Phase =
  | { kind: 'loading' }
  | { kind: 'error'; error: ApiError }
  | { kind: 'setup' }
  | { kind: 'login'; notice?: string }
  | { kind: 'in'; user: AdminUser };

export interface AppProps {
  pages: PageDef[];
  /** For tests: the fetch and origin of the API client. The page uses its own origin. */
  fetch?: ApiClientOptions['fetch'];
  baseUrl?: string;
}

/**
 * The admin UI: first-run setup or login, then the layout with the registered pages. A 401 while
 * logged in (the session ended or was revoked) returns to the login page. The session cookie is
 * HttpOnly and set by the server; the UI never reads it.
 */
export function App({ pages, fetch, baseUrl }: AppProps) {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' });
  const phaseRef = useRef(phase);
  useEffect(() => {
    phaseRef.current = phase;
  });

  const client = useMemo(
    () =>
      createApiClient({
        baseUrl,
        fetch,
        onUnauthorized: () => {
          if (phaseRef.current.kind === 'in') {
            phaseRef.current = { kind: 'login' };
            setPhase({ kind: 'login', notice: 'Your session ended. Log in again.' });
          }
        },
      }),
    [baseUrl, fetch],
  );

  const start = useCallback(async () => {
    setPhase({ kind: 'loading' });
    try {
      const setup = await call(client.GET('/admin/v1/setup'));
      if (!setup.admin_exists) {
        setPhase({ kind: 'setup' });
        return;
      }
      try {
        setPhase({ kind: 'in', user: await call(client.GET('/admin/v1/session')) });
      } catch (e) {
        if (!isUnauthorized(e)) throw e;
        setPhase({ kind: 'login' });
      }
    } catch (e) {
      setPhase({ kind: 'error', error: asApiError(e) });
    }
  }, [client]);

  useEffect(() => {
    void start();
  }, [start]);

  const loggedIn = useCallback((user: AdminUser) => setPhase({ kind: 'in', user }), []);

  const logout = useCallback(async () => {
    phaseRef.current = { kind: 'login' };
    try {
      await call(client.DELETE('/admin/v1/session'));
      setPhase({ kind: 'login', notice: 'You are logged out.' });
    } catch (e) {
      // Out either way: a session the server no longer knows is as good as ended.
      setPhase({ kind: 'login', notice: isUnauthorized(e) ? 'You are logged out.' : asApiError(e).message });
    }
  }, [client]);

  let body;
  switch (phase.kind) {
    case 'loading':
      body = (
        <div className="p-8">
          <Spinner />
        </div>
      );
      break;
    case 'error':
      body = (
        <div className="mx-auto mt-16 flex max-w-md flex-col gap-3">
          <ErrorBanner error={phase.error} title="The admin API did not answer" />
          <Button onClick={() => void start()}>Try again</Button>
        </div>
      );
      break;
    case 'setup':
      body = <SetupForm onCreated={loggedIn} />;
      break;
    case 'login':
      body = <LoginForm onLoggedIn={loggedIn} notice={phase.notice} />;
      break;
    case 'in':
      body = (
        <SessionProvider value={{ user: phase.user, logout }}>
          <SettingsProvider>
            <Layout pages={pages} />
          </SettingsProvider>
        </SessionProvider>
      );
      break;
  }
  return <ApiProvider value={client}>{body}</ApiProvider>;
}
