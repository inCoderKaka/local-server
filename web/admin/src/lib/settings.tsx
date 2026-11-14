import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { call, parseGoDuration, useApi, useClient, type ApiError, type Schema } from '@local-server/shared';
import { SETTING } from './settingKeys';

export type Setting = Schema<'Setting'>;

interface SettingsState {
  /** The host-owned settings, once loaded. */
  list: Setting[] | undefined;
  error: ApiError | null;
  loading: boolean;
  /** Reads the settings again. */
  reload: () => Promise<void>;
  /** Replaces the list with the one a PATCH answered, so every reader sees the saved values. */
  replace: (list: Setting[]) => void;
}

const SettingsContext = createContext<SettingsState | null>(null);

/**
 * Reads the host-owned settings once when the session starts (GET /admin/v1/settings) and holds
 * them for the shell. A page that saves settings calls `replace` with the answer, so values the
 * shell derives from them (the poll interval) follow at once.
 */
export function SettingsProvider({ children }: { children: ReactNode }) {
  const client = useClient();
  const s = useApi(async () => (await call(client.GET('/admin/v1/settings'))).data, [client]);
  const value = useMemo<SettingsState>(
    () => ({ list: s.data, error: s.error, loading: s.loading, reload: s.refetch, replace: s.setData }),
    [s.data, s.error, s.loading, s.refetch, s.setData],
  );
  return <SettingsContext.Provider value={value}>{children}</SettingsContext.Provider>;
}

export function useSettings(): SettingsState {
  const c = useContext(SettingsContext);
  if (!c) throw new Error('useSettings needs a SettingsProvider above it');
  return c;
}

export function settingOf(list: Setting[] | undefined, key: string): Setting | undefined {
  return list?.find((s) => s.key === key);
}

/**
 * The poll interval in milliseconds from admin.ui_poll_interval: its value, or, when that does not
 * parse as a positive Go duration, the setting's own default_value from the API. Null (no polling)
 * when neither parses or the setting is missing; the UI never invents a number (req_001 D-38).
 */
export function pollIntervalFrom(list: Setting[] | undefined): number | null {
  const s = settingOf(list, SETTING.uiPollInterval);
  if (!s) return null;
  for (const v of [s.value, s.default_value]) {
    const ms = parseGoDuration(v);
    if (ms !== null && ms > 0) return ms;
  }
  return null;
}

/** The poll interval of the shell (see pollIntervalFrom). */
export function usePollInterval(): number | null {
  return pollIntervalFrom(useSettings().list);
}

/**
 * The memory budget in bytes from scheduler.memory_budget_bytes, or null when the setting is
 * missing or not a whole number of 0 or more. 0 means no limit.
 */
export function memoryBudgetFrom(list: Setting[] | undefined): number | null {
  const v = settingOf(list, SETTING.memoryBudgetBytes)?.value;
  return typeof v === 'number' && Number.isInteger(v) && v >= 0 ? v : null;
}
