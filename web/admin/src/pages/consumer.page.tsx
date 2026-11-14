import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'consumer',
  path: '/consumers/:consumerId',
  label: 'Consumer',
  order: 12,
  navVisible: false,
  element: lazy(() => import('../consumers/ConsumerPage')),
};
