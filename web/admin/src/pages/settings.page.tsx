import { lazy } from 'react';
import type { PageDef } from '../registry';

export const page: PageDef = {
  id: 'settings',
  path: '/settings',
  label: 'Settings',
  order: 32,
  element: lazy(() => import('../settings/SettingsPage')),
};
