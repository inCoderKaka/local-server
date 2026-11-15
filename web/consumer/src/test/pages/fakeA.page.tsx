import { useState } from 'react';
import { asApiError, Button, call, ErrorBanner, useClient } from '@local-server/shared';
import type { PageDef } from '../../registry';

// A fixture page that calls the API, so a test can end the session under it.
function FakeA() {
  const client = useClient();
  const [error, setError] = useState<unknown>(null);
  return (
    <>
      <h1>Fake page A</h1>
      <Button
        onClick={() => {
          call(client.GET('/v1/me/limits')).catch((e: unknown) => setError(asApiError(e)));
        }}
      >
        Load limits
      </Button>
      <ErrorBanner error={error} />
    </>
  );
}

export const page: PageDef = { id: 'fake-a', path: '/fake-a', label: 'Fake A', order: 20, element: FakeA };
