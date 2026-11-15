import { lazy } from 'react';
import type { PageDef } from '../registry';
import { REQUESTS_PATH } from '../requests/pending';

export const page: PageDef = {
  id: 'requests',
  path: REQUESTS_PATH,
  label: 'Requests',
  order: 33,
  element: lazy(() => import('../requests/RequestsPage')),
};
