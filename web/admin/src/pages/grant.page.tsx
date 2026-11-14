import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'grant',
  path: '/consumers/:consumerId/grant',
  label: 'Grant',
  order: 20,
  navVisible: false,
  element: lazy(() => import('../grants/GrantPage')),
};
