import { parseGoDuration } from './duration';

// Readers of what a person typed in a form field. Each returns the value or a plain-language error.

/** A duration a person typed (`30s`, `5m`, `24h`, `1h30m`) as whole seconds of 0 or more. */
export function secondsFrom(text: string): { value?: number; error?: string } {
  const ms = parseGoDuration(text.trim());
  if (ms === null) return { error: 'Write a duration such as 30s, 5m, 24h or 1h30m.' };
  if (ms < 0) return { error: 'The duration cannot be negative.' };
  if (ms % 1000 !== 0) return { error: 'Use whole seconds.' };
  return { value: ms / 1000 };
}

/**
 * A whole number a person typed, at least `min`. Empty is `{}` (the caller decides what empty
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

/** A comma-separated list a person typed, trimmed, without empty entries. */
export function listFrom(text: string): string[] {
  return text
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s !== '');
}
