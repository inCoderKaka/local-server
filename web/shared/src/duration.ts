/**
 * Parses a Go duration string (`30s`, `1m`, `90s`, `1h30m`, `1.5h`, `250ms`, `0`) the way Go's
 * time.ParseDuration does, and returns it in milliseconds, or null when it does not parse.
 * Settings are stored as Go durations (req_001 D-38); the UI never invents a fallback value.
 */
export function parseGoDuration(input: unknown): number | null {
  if (typeof input !== 'string') return null;
  let s = input;
  let sign = 1;
  if (s.startsWith('-') || s.startsWith('+')) {
    if (s[0] === '-') sign = -1;
    s = s.slice(1);
  }
  if (s === '0') return 0;
  if (s === '') return null;
  let total = 0;
  const re = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/;
  while (s !== '') {
    const m = re.exec(s);
    if (!m || m[1] === undefined || m[2] === undefined) return null;
    const n = Number(m[1]);
    if (!Number.isFinite(n)) return null;
    total += n * unitMs[m[2]]!;
    s = s.slice(m[0].length);
  }
  return sign * total;
}

const unitMs: Record<string, number> = {
  ns: 1e-6,
  us: 1e-3,
  µs: 1e-3,
  μs: 1e-3,
  ms: 1,
  s: 1000,
  m: 60_000,
  h: 3_600_000,
};

/** Whole seconds as a duration a person can read and type again: 86400 is `24h`, 5400 `1h30m`. */
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
