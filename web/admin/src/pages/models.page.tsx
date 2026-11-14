import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'models',
  path: '/models',
  label: 'Models',
  order: 21,
  element: lazy(() => import('../models/ModelsPage')),
};
