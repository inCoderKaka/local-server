import { createContext, useContext } from 'react';
import type { Schema } from '@local-server/shared';

export type AdminUser = Schema<'AdminUser'>;

interface Session {
  user: AdminUser;
  /** Ends the session (DELETE /admin/v1/session) and returns to the login page. */
  logout: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export const SessionProvider = SessionContext.Provider;

/** The logged-in admin and the logout action. */
export function useSession(): Session {
  const c = useContext(SessionContext);
  if (!c) throw new Error('useSession needs a SessionProvider above it');
  return c;
}
