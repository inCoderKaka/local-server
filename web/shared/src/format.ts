/** Shown for a value that could not be measured: never 0 and never a guess (req_001 D-31). */
export const NOT_AVAILABLE = 'n/a';

function missing(v: number | null | undefined): v is null | undefined {
  return v === null || v === undefined || !Number.isFinite(v);
}

const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

/** Formats a byte count in binary units (`16.0 GiB`); null is `n/a`. */
export function formatBytes(v: number | null | undefined): string {
  if (missing(v)) return NOT_AVAILABLE;
  let n = Math.abs(v);
  let i = 0;
  while (n >= 1024 && i < byteUnits.length - 1) {
    n /= 1024;
    i++;
  }
  const sign = v < 0 ? '-' : '';
  return i === 0 ? `${sign}${n} B` : `${sign}${n.toFixed(1)} ${byteUnits[i]}`;
}

/** Formats a percentage with one decimal (`12.5%`); null is `n/a`. */
export function formatPercent(v: number | null | undefined): string {
  if (missing(v)) return NOT_AVAILABLE;
  return `${v.toFixed(1)}%`;
}

function toTime(v: string | Date | null | undefined): number | null {
  if (v === null || v === undefined) return null;
  const t = v instanceof Date ? v.getTime() : Date.parse(v);
  return Number.isNaN(t) ? null : t;
}

const steps: [Intl.RelativeTimeFormatUnit, number][] = [
  ['second', 60],
  ['minute', 60],
  ['hour', 24],
  ['day', 7],
  ['week', 4.348],
  ['month', 12],
  ['year', Number.POSITIVE_INFINITY],
];

/**
 * Formats a time relative to now (`5 minutes ago`, `in 2 hours`) in the given locale (the
 * browser's by default); null or an unreadable time is `n/a`.
 */
export function formatRelativeTime(
  v: string | Date | null | undefined,
  now: number = Date.now(),
  locale?: string,
): string {
  const t = toTime(v);
  if (t === null) return NOT_AVAILABLE;
  let n = (t - now) / 1000;
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  for (const [unit, size] of steps) {
    if (Math.abs(n) < size) return rtf.format(Math.round(n), unit);
    n /= size;
  }
  return rtf.format(Math.round(n), 'year');
}

/** Formats a time as a local date and time; null or an unreadable time is `n/a`. */
export function formatDateTime(v: string | Date | null | undefined, locale?: string): string {
  const t = toTime(v);
  if (t === null) return NOT_AVAILABLE;
  return new Date(t).toLocaleString(locale);
}
