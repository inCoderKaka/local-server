import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'keys',
  path: '/keys',
  label: 'My keys',
  order: 30,
  element: lazy(() => import('../keys/KeysPage')),
};
