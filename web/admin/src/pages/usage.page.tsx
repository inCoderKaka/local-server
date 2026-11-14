import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'usage',
  path: '/usage',
  label: 'Usage',
  order: 30,
  element: lazy(() => import('../usage/UsagePage')),
};
