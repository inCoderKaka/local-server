import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'audit',
  path: '/audit',
  label: 'Audit log',
  order: 31,
  element: lazy(() => import('../audit/AuditPage')),
};
