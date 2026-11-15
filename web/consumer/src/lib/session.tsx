import { createContext, useContext } from 'react';
import type { Schema } from '@local-server/shared';

export type Consumer = Schema<'Consumer'>;

interface Session {
  /** The logged-in consumer (GET /v1/me, or the answer of the sign-up or login). */
  consumer: Consumer;
  /** Ends the session (DELETE /v1/session) and returns to the login page. */
  logout: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export const SessionProvider = SessionContext.Provider;

/** The logged-in consumer and the logout action. */
export function useSession(): Session {
  const c = useContext(SessionContext);
  if (!c) throw new Error('useSession needs a SessionProvider above it');
  return c;
}

/** The name to greet a consumer with: the display name, else the login name, else none. */
export function consumerName(c: Consumer): string | null {
  return c.display_name || c.username || null;
}
