import { Card, Link } from '@local-server/shared';

/** The page for a route no page is registered for. */
export function NotFound({ path, home = '/', homeLabel = 'Go to the start page' }: { path: string; home?: string; homeLabel?: string }) {
  return (
    <Card title="Page not found">
      <p className="text-sm">
        No page has the address <code>{path}</code>. <Link to={home}>{homeLabel}</Link>.
      </p>
    </Card>
  );
}
