import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ApiProvider,
  asApiError,
  Button,
  call,
  createApiClient,
  ErrorBanner,
  isUnauthorized,
  matchRoute,
  navigate,
  Spinner,
  useHashLocation,
  type ApiClientOptions,
  type ApiError,
} from '@local-server/shared';
import { LoginPage } from './auth/LoginPage';
import { SignupPage } from './auth/SignupPage';
import { Layout } from './layout/Layout';
import { NotFound } from './layout/NotFound';
import { SessionProvider, type Consumer } from './lib/session';
import { shellPaths, type PageDef } from './registry';

type Phase =
  | { kind: 'loading' }
  | { kind: 'error'; error: ApiError }
  | { kind: 'out'; notice?: string }
  | { kind: 'in'; consumer: Consumer };

export interface AppProps {
  pages: PageDef[];
  /** The token of the invite link the page was opened with (see takeInviteFromAddress), if any. */
  initialInvite?: string | null;
  /** For tests: the fetch and origin of the API client. The page uses its own origin. */
  fetch?: ApiClientOptions['fetch'];
  baseUrl?: string;
}

function isShellPath(path: string): boolean {
  return (shellPaths as readonly string[]).includes(path.replace(/\/+$/, ''));
}

/**
 * The consumer web page: while nobody is logged in, the sign-up (`#/signup`) and login forms;
 * after login, the layout with the registered pages. A page route opened while logged out shows
 * the login form and then that page. A 401 while logged in (the session ended or was revoked)
 * returns to the login form. The session cookie is HttpOnly and set by the host; the page never
 * reads it, and it keeps no prompt, response or secret in storage.
 */
export function App({ pages, initialInvite, fetch, baseUrl }: AppProps) {
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' });
  const [invite] = useState(initialInvite ?? null);
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
            phaseRef.current = { kind: 'out' };
            setPhase({ kind: 'out', notice: 'Your session ended. Log in again.' });
          }
        },
      }),
    [baseUrl, fetch],
  );

  const start = useCallback(async () => {
    setPhase({ kind: 'loading' });
    try {
      setPhase({ kind: 'in', consumer: await call(client.GET('/v1/me')) });
    } catch (e) {
      if (isUnauthorized(e)) setPhase({ kind: 'out' });
      else setPhase({ kind: 'error', error: asApiError(e) });
    }
  }, [client]);

  useEffect(() => {
    void start();
  }, [start]);

  const signedIn = useCallback((consumer: Consumer) => setPhase({ kind: 'in', consumer }), []);

  const logout = useCallback(async () => {
    phaseRef.current = { kind: 'out' };
    try {
      await call(client.DELETE('/v1/session'));
      setPhase({ kind: 'out', notice: 'You are logged out.' });
    } catch (e) {
      // Out either way: a session the host no longer knows is as good as ended.
      setPhase({ kind: 'out', notice: isUnauthorized(e) ? 'You are logged out.' : asApiError(e).message });
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
        <div className="mx-auto mt-16 flex max-w-md flex-col gap-3 px-4">
          <ErrorBanner error={phase.error} title="The host did not answer" />
          <Button onClick={() => void start()}>Try again</Button>
        </div>
      );
      break;
    case 'out':
      body = <SignedOut pages={pages} invite={invite} notice={phase.notice} onSignedIn={signedIn} />;
      break;
    case 'in':
      body = (
        <SessionProvider value={{ consumer: phase.consumer, logout }}>
          <SignedIn pages={pages} />
        </SessionProvider>
      );
      break;
  }
  return <ApiProvider value={client}>{body}</ApiProvider>;
}

/** The routes while nobody is logged in: sign-up, login (also for any page route), else not found. */
function SignedOut({
  pages,
  invite,
  notice,
  onSignedIn,
}: {
  pages: PageDef[];
  invite: string | null;
  notice?: string;
  onSignedIn: (c: Consumer) => void;
}) {
  const { path } = useHashLocation();
  const signup = path.replace(/\/+$/, '') === '/signup';
  const login = !signup && (path === '/' || isShellPath(path) || matchRoute(pages, path) !== null);
  const title = signup ? 'Sign up' : login ? 'Log in' : 'Page not found';

  useEffect(() => {
    document.title = `${title} · local-server`;
  }, [title]);

  if (signup) return <SignupPage initialToken={invite} onSignedIn={onSignedIn} />;
  if (login) return <LoginPage onSignedIn={onSignedIn} notice={notice} />;
  return (
    <div className="mx-auto mt-16 max-w-md px-4">
      <NotFound path={path} home="/login" homeLabel="Go to the login page" />
    </div>
  );
}

/** The routes after login: the sign-up and login routes go to the start page; the rest is the layout. */
function SignedIn({ pages }: { pages: PageDef[] }) {
  const { path } = useHashLocation();
  const leave = isShellPath(path);

  useEffect(() => {
    if (leave) navigate('/');
  }, [leave]);

  if (leave) {
    return (
      <div className="p-8">
        <Spinner />
      </div>
    );
  }
  return <Layout pages={pages} />;
}
