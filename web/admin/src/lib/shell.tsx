import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { call, usePolledApi, useClient, type ApiState, type Schema } from '@local-server/shared';
import { usePollInterval } from './settings';

export type HostInfo = Schema<'HostInfo'>;
export type HostMetricsSample = Schema<'HostMetricsSample'>;

interface ShellData {
  /** GET /admin/v1/host, polled: sharing state, engine and tailnet. */
  host: ApiState<HostInfo>;
  /** GET /admin/v1/host-metrics, polled: total, share-budget slice, other apps, unattributed. */
  metrics: ApiState<HostMetricsSample>;
}

const ShellContext = createContext<ShellData | null>(null);

/**
 * Polls the host state and the current host metrics once for the whole shell, every
 * admin.ui_poll_interval; the header and any page read the same data through useShell.
 */
export function ShellDataProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const interval = usePollInterval();
  const host = usePolledApi(() => call(client.GET('/admin/v1/host')), [client], interval);
  const metrics = usePolledApi(() => call(client.GET('/admin/v1/host-metrics')), [client], interval);
  const value = useMemo(() => ({ host, metrics }), [host, metrics]);
  return <ShellContext.Provider value={value}>{children}</ShellContext.Provider>;
}

export function useShell(): ShellData {
  const c = useContext(ShellContext);
  if (!c) throw new Error('useShell needs a ShellDataProvider above it');
  return c;
}
