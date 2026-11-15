import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'settings',
  path: '/settings',
  label: 'Settings',
  order: 50,
  element: lazy(() => import('../settings/SettingsPage')),
};
