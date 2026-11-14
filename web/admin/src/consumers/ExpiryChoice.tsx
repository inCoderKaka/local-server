import { useId } from 'react';
import { Field } from '@local-server/shared';

export interface Expiry {
  /** `default`: send no expiry, so the API's own rule applies; `at`: the chosen moment. */
  mode: 'default' | 'at';
  /** The value of the date and time input (`YYYY-MM-DDTHH:MM`, local time). */
  at: string;
}

export const noExpiry: Expiry = { mode: 'default', at: '' };

/**
 * The expiry as the API wants it: undefined for the API's default, an RFC 3339 time for a
 * chosen moment, or an error message for a moment that is missing or unreadable. The UI offers no
 * preset durations: the default is the server's (req_001 D-38).
 */
export function expiryValue(e: Expiry): { value?: string; error?: string } {
  if (e.mode === 'default') return {};
  if (!e.at) return { error: 'Choose a date and time.' };
  const t = new Date(e.at);
  if (Number.isNaN(t.getTime())) return { error: 'The date and time cannot be read.' };
  return { value: t.toISOString() };
}

/** Two choices for when something expires: the API's default, or a date and time. */
export function ExpiryChoice({
  value,
  onChange,
  defaultLabel,
  error,
}: {
  value: Expiry;
  onChange: (e: Expiry) => void;
  /** The label of the default choice, for example "Never". */
  defaultLabel: string;
  error?: string;
}) {
  const name = useId();
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium text-slate-800">Expires</legend>
      <label className="flex items-center gap-2 text-sm">
        <input type="radio" name={name} checked={value.mode === 'default'} onChange={() => onChange({ ...value, mode: 'default' })} />
        {defaultLabel}
      </label>
      <label className="flex items-center gap-2 text-sm">
        <input type="radio" name={name} checked={value.mode === 'at'} onChange={() => onChange({ ...value, mode: 'at' })} />
        At a date and time
      </label>
      {value.mode === 'at' && (
        <Field
          label="Expiry date and time"
          hint="In this browser's time zone."
          type="datetime-local"
          value={value.at}
          onChange={(e) => onChange({ ...value, at: e.target.value })}
          error={error}
        />
      )}
      {value.mode === 'default' && error && <p className="text-xs text-red-700">{error}</p>}
    </fieldset>
  );
}
