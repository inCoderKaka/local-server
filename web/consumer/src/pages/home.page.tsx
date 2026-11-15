import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'home',
  path: '/',
  label: 'Home',
  order: 0,
  element: lazy(() => import('../home/HomePage')),
};
