/** Keys of the host-owned settings the admin shell reads or writes (GET/PATCH /admin/v1/settings). */
export const SETTING = {
  /** How often the shell refreshes the resource strip, sharing state and pending count (D-157). */
  uiPollInterval: 'admin.ui_poll_interval',
  /** The memory budget in bytes; 0 means no limit. */
  memoryBudgetBytes: 'scheduler.memory_budget_bytes',
  quietEnabled: 'host.quiet_hours.enabled',
  quietTimeZone: 'host.quiet_hours.time_zone',
  quietStart: 'host.quiet_hours.start',
  quietEnd: 'host.quiet_hours.end',
} as const;
