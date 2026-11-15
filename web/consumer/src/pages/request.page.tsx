import { lazy } from 'react';
import type { PageDef } from '../registry';
import { REQUESTS_PATH } from '../lib/requests';

export const page: PageDef = {
  id: 'request',
  path: `${REQUESTS_PATH}/:requestId`,
  label: 'Request',
  order: 41,
  element: lazy(() => import('../requests/RequestPage')),
  navVisible: false,
};
