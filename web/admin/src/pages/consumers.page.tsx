import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'consumers',
  path: '/consumers',
  label: 'Consumers',
  order: 10,
  element: lazy(() => import('../consumers/ConsumersPage')),
};
