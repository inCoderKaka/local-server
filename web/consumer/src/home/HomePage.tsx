import { Card, UserText } from '@local-server/shared';
import { consumerName, useSession } from '../lib/session';

/** The start page after login: who is logged in. The account pages are added beside it. */
export default function HomePage() {
  const { consumer } = useSession();
  const name = consumerName(consumer);
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
        </p>
      </Card>
    </>
  );
}
