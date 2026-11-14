import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'policy-rules',
  path: '/policy-rules',
  label: 'Rules',
  order: 22,
  element: lazy(() => import('../policy/PolicyRulesPage')),
};
