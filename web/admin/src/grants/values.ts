import { parseGoDuration } from '@local-server/shared';
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

/** Whole seconds as a duration the host can read and type again: 86400 is `24h`, 5400 `1h30m`. */
export function formatSeconds(total: number): string {
  if (!Number.isFinite(total)) return '';
  let s = Math.abs(Math.trunc(total));
  if (s === 0) return '0s';
  const h = Math.floor(s / 3600);
  s -= h * 3600;
  const m = Math.floor(s / 60);
  s -= m * 60;
  return (total < 0 ? '-' : '') + (h ? `${h}h` : '') + (m ? `${m}m` : '') + (s ? `${s}s` : '');
}

/** A duration the host typed (`30s`, `5m`, `24h`, `1h30m`) as whole seconds of 0 or more. */
export function secondsFrom(text: string): { value?: number; error?: string } {
  const ms = parseGoDuration(text.trim());
  if (ms === null) return { error: 'Write a duration such as 30s, 5m, 24h or 1h30m.' };
  if (ms < 0) return { error: 'The duration cannot be negative.' };
  if (ms % 1000 !== 0) return { error: 'Use whole seconds.' };
  return { value: ms / 1000 };
}

/**
 * A whole number the host typed, at least `min`. Empty is `{}` (the caller decides what empty
 * means); anything else that is not such a number is an error.
 */
export function wholeFrom(text: string, min: number): { value?: number; error?: string } {
  const t = text.trim();
  if (t === '') return {};
  if (!/^\d+$/.test(t) || !Number.isSafeInteger(Number(t))) return { error: 'Write a whole number.' };
  const n = Number(t);
  if (n < min) return { error: `Write ${min} or more.` };
  return { value: n };
}

/** A comma-separated list the host typed, trimmed, without empty entries. */
export function listFrom(text: string): string[] {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '');
}

/** An RFC 3339 time as the value of a datetime-local input, in this browser's time zone. */
export function localInputOf(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}
