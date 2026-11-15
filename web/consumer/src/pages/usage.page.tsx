import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'usage',
  path: '/usage',
  label: 'My usage',
  order: 20,
  element: lazy(() => import('../usage/UsagePage')),
};
