import { lazy } from 'react';
import type { PageDef } from '../registry';
import { REQUESTS_PATH } from '../lib/requests';

export const page: PageDef = {
  id: 'requests',
  path: REQUESTS_PATH,
  label: 'Requests',
  order: 40,
  element: lazy(() => import('../requests/RequestsPage')),
};
