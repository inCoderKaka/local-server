import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'dashboard',
  path: '/',
  label: 'Dashboard',
  order: 0,
  element: lazy(() => import('../dashboard/Dashboard')),
};
