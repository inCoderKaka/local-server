import { createContext, useContext } from 'react';
import type { ApiClient } from './client';

const ApiContext = createContext<ApiClient | null>(null);

/** Provides the API client to the components below it. */
export const ApiProvider = ApiContext.Provider;

/** The API client of the nearest ApiProvider. */
export function useClient(): ApiClient {
  const c = useContext(ApiContext);
  if (!c) throw new Error('useClient needs an ApiProvider above it');
  return c;
}
