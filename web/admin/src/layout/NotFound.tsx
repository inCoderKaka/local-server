import { Card, Link } from '@local-server/shared';

/** The page for a route no page is registered for. */
export function NotFound({ path }: { path: string }) {
  return (
    <Card title="Page not found">
      <p className="text-sm">
        No page has the address <code>{path}</code>. <Link to="/">Go to the dashboard</Link>.
      </p>
    </Card>
  );
}
