import { Suspense, useEffect } from 'react';
import { Button, Link, matchRoute, ParamsProvider, Spinner, UserText, useHashLocation } from '@local-server/shared';
import type { PageDef } from '../registry';
import { consumerName, useSession } from '../lib/session';
import { NotFound } from './NotFound';

/** The frame of every page after login: the navigation of the registered pages, the account, the page. */
export function Layout({ pages }: { pages: PageDef[] }) {
  const { path } = useHashLocation();
  const { consumer, logout } = useSession();
  const match = matchRoute(pages, path);
  const nav = pages.filter((p) => p.navVisible !== false);
  const title = match ? match.route.label : 'Page not found';

  useEffect(() => {
    document.title = `${title} · local-server`;
  }, [title]);

  const Page = match?.route.element;
  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="bg-white shadow-sm">
        <div className="flex flex-wrap items-center gap-4 px-4 py-2">
          <span className="font-semibold">local-server</span>
          <nav aria-label="Main" className="flex flex-wrap gap-3 text-sm">
            {nav.map((p) => (
              <Link key={p.id} to={p.path} className="text-slate-700 hover:text-slate-950 aria-[current=page]:font-semibold">
                {p.label}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <UserText text={consumerName(consumer)} className="text-slate-600" />
            <Button onClick={() => void logout()}>Log out</Button>
          </div>
        </div>
      </header>
      <main className="mx-auto flex max-w-4xl flex-col gap-4 p-4">
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
  );
}
