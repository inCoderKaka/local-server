import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'limits',
  path: '/limits',
  label: 'My limits',
  order: 10,
  element: lazy(() => import('../limits/LimitsPage')),
};
