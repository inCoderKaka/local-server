import { Suspense, useEffect } from 'react';
import { Button, ErrorBanner, Link, matchRoute, ParamsProvider, Spinner, useHashLocation } from '@local-server/shared';
import type { PageDef } from '../registry';
import { useSession } from '../lib/session';
import { useSettings } from '../lib/settings';
import { ShellDataProvider } from '../lib/shell';
import { NotFound } from './NotFound';
import { PendingRequestsBadge } from './PendingRequestsBadge';
import { ResourceStrip } from './ResourceStrip';
import { SharingBadge } from './SharingBadge';

/** The frame of every page after login: navigation, state badges, resource strip, the page. */
export function Layout({ pages }: { pages: PageDef[] }) {
  const { path } = useHashLocation();
  const { user, logout } = useSession();
  const settings = useSettings();
  const match = matchRoute(pages, path);
  const nav = pages.filter((p) => p.navVisible !== false);
  const title = match ? match.route.label : 'Page not found';

  useEffect(() => {
    document.title = `${title} · local-server admin`;
  }, [title]);

  const Page = match?.route.element;
  return (
    <ShellDataProvider>
      <div className="min-h-screen bg-slate-50 text-slate-900">
        <header className="bg-white shadow-sm">
          <div className="flex flex-wrap items-center gap-4 px-4 py-2">
            <span className="font-semibold">local-server admin</span>
            <nav aria-label="Main" className="flex flex-wrap gap-3 text-sm">
              {nav.map((p) => (
                <Link key={p.id} to={p.path} className="text-slate-700 hover:text-slate-950 aria-[current=page]:font-semibold">
                  {p.label}
                </Link>
              ))}
            </nav>
            <div className="ml-auto flex items-center gap-3 text-sm">
              <SharingBadge />
              <PendingRequestsBadge />
              <span className="text-slate-600">{user.username}</span>
              <Button onClick={() => void logout()}>Log out</Button>
            </div>
          </div>
          <ResourceStrip />
        </header>
        <main className="mx-auto flex max-w-6xl flex-col gap-4 p-4">
          <ErrorBanner error={settings.error} title="The host settings could not be read" />
          {Page && match ? (
            <ParamsProvider value={match.params}>
              <Suspense fallback={<Spinner />}>
                <Page key={match.route.id} />
              </Suspense>
            </ParamsProvider>
          ) : (
            <NotFound path={path} />
          )}
        </main>
      </div>
    </ShellDataProvider>
  );
}
