import { Badge, Link } from '@local-server/shared';
import { useShell } from '../lib/shell';
import { REQUESTS_PATH } from '../requests/pending';

/**
 * The pending resource requests on every page. The shell polls the count every
 * admin.ui_poll_interval (see ShellDataProvider), and the requests page refreshes it after a
 * decision, so the badge and the page show the same number.
 */
export function PendingRequestsBadge() {
  const { data, error } = useShell().pending;
  if (error && !data) {
    return (
      <span title={error.message}>
        <Badge tone="neutral">Requests: n/a</Badge>
      </span>
    );
  }
  if (!data || data.kind === 'unavailable') return null;
  const label = `${data.n} pending resource ${data.n === 1 ? 'request' : 'requests'}`;
  return (
    <Link to={REQUESTS_PATH} aria-label={label} className="no-underline">
      <Badge tone={data.n > 0 ? 'warn' : 'neutral'}>Requests {data.n}</Badge>
    </Link>
  );
}
