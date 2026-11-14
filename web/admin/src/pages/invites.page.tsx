import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'invites',
  path: '/invites',
  label: 'Invites',
  order: 11,
  element: lazy(() => import('../invites/InvitesPage')),
};
