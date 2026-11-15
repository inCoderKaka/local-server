import { useState } from 'react';
import { Card, ErrorBanner, Link, Spinner, UserText, useApi, useClient } from '@local-server/shared';
import { limitsLine, useMyLimits } from '../lib/limits';
import { countOpenRequests, REQUESTS_PATH } from '../lib/requests';
import { consumerName, useSession } from '../lib/session';
import { useCurrentUsage } from '../usage/currentUsage';
import { CurrentUsageView } from '../usage/CurrentUsageView';

/**
 * The start page after login: who is logged in and a short summary that links to the pages: the
 * limits in one line, what is used now, and how many requests are pending.
 */
export default function HomePage() {
  const client = useClient();
  const { consumer } = useSession();
  const name = consumerName(consumer);
  const [now] = useState(() => Date.now());
  const limits = useMyLimits();
  const usage = useCurrentUsage(limits.data === undefined ? undefined : (limits.data?.limits ?? null), now);
  const open = useApi(() => countOpenRequests(client), [client]);

  return (
    <>
      <h1 className="text-xl font-semibold">Home</h1>
      <Card title="Your account">
        <p className="text-sm">
          {name ? (
            <>
              You are logged in as <UserText text={name} className="font-medium" />.
            </>
          ) : (
            'You are logged in.'
          )}
          {consumer.status === 'suspended' && ' The host has suspended this account: you can read your pages, but your keys are refused.'}
        </p>
      </Card>
      <Card title="Your limits" actions={<Link to="/limits">My limits</Link>}>
        <ErrorBanner error={limits.error} title="Your limits could not be read" />
        {limits.loading && limits.data === undefined && <Spinner label="Loading your limits" />}
        {limits.data === null && (
          <p className="text-sm">
            The host has not given you access yet. <Link to={REQUESTS_PATH}>Ask for access</Link>.
          </p>
        )}
        {limits.data && (
          <p className="text-sm">
            <UserText text={limitsLine(limits.data.limits)} />
          </p>
        )}
      </Card>
      <Card title="Used now" actions={<Link to="/usage">My usage</Link>}>
        <ErrorBanner error={usage.error} title="Your usage could not be read" />
        {usage.loading && !usage.data && <Spinner label="Loading your usage" />}
        {usage.data && <CurrentUsageView usage={usage.data} />}
      </Card>
      <Card title="Requests" actions={<Link to={REQUESTS_PATH}>Requests</Link>}>
        <ErrorBanner error={open.error} title="Your requests could not be read" />
        {open.loading && open.data === undefined && <Spinner label="Loading your requests" />}
        {open.data !== undefined && (
          <p className="text-sm">
            {open.data === 0 ? 'You have no pending requests.' : open.data === 1 ? 'You have 1 pending request.' : `You have ${open.data} pending requests.`}
          </p>
        )}
      </Card>
    </>
  );
}
