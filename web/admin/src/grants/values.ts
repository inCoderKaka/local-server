import { settingOf, type Setting } from '../lib/settings';

/**
 * Keys of host-owned settings the grant, model and rule pages read, only to say what applies when
 * a field is left empty or to start a new field from the host's own value (req_001 D-38). They
 * belong to other modules' settings; these pages never write them.
 */
export const HOST_SETTING = {
  /** The concurrency a grant that sets none gets. */
  grantConcurrency: 'scheduler.default_grant_concurrency',
  /** The context size sent to the engine when neither the grant nor a rule sets a context limit. */
  contextTokens: 'gateway.default_context_tokens',
  /** The output cap when neither the grant nor a rule sets one. */
  maxTokensCeiling: 'gateway.max_tokens_ceiling',
  /** The host's quota window, and the bounds of a window a quota entry may have. */
  quotaWindow: 'limits.quota_window',
  quotaWindowMin: 'limits.quota_window_min',
  quotaWindowMax: 'limits.quota_window_max',
  /** How long an idle model stays loaded. */
  keepAlive: 'scheduler.keep_alive',
  /** Whether a model the host sees for the first time may be put in grants at once. */
  modelDefaultAllowed: 'admin.model_default_allowed',
  /** Failed evaluations before the host switches off a broken rule without deny. */
  ruleMaxFailures: 'policy.rule.max_failures',
} as const;

/** An integer setting's value, or null when it is missing or not a whole number. */
export function intSetting(list: Setting[] | undefined, key: string): number | null {
  const v = settingOf(list, key)?.value;
  const n = typeof v === 'string' && /^-?\d+$/.test(v) ? Number(v) : v;
  return typeof n === 'number' && Number.isSafeInteger(n) ? n : null;
}

/** A text setting's value (a duration, a name), or null when it is missing or empty. */
export function textSetting(list: Setting[] | undefined, key: string): string | null {
  const v = settingOf(list, key)?.value;
  return typeof v === 'string' && v !== '' ? v : null;
}

/** A boolean setting's value, or null when it is missing. */
export function boolSetting(list: Setting[] | undefined, key: string): boolean | null {
  const v = settingOf(list, key)?.value;
  return typeof v === 'boolean' ? v : null;
}

/** An RFC 3339 time as the value of a datetime-local input, in this browser's time zone. */
export function localInputOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
