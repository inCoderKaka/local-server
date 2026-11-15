import { describe, expect, it } from 'vitest';
import { formatBytes, formatCount, formatDateTime, formatPercent, formatRelativeTime, NOT_AVAILABLE } from './format';

describe('formatters', () => {
  it('formats bytes in binary units', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KiB');
    expect(formatBytes(17179869184)).toBe('16.0 GiB');
    expect(formatBytes(8589934592)).toBe('8.0 GiB');
  });

  it('formats percentages', () => {
    expect(formatPercent(12.5)).toBe('12.5%');
    expect(formatPercent(0)).toBe('0.0%');
  });

  it('shows n/a for a value that could not be read, never 0', () => {
    for (const f of [formatBytes, formatPercent]) {
      expect(f(null)).toBe(NOT_AVAILABLE);
      expect(f(undefined)).toBe(NOT_AVAILABLE);
      expect(f(Number.NaN)).toBe(NOT_AVAILABLE);
    }
    expect(NOT_AVAILABLE).toBe('n/a');
    expect(formatRelativeTime(null)).toBe('n/a');
    expect(formatRelativeTime('not a time')).toBe('n/a');
    expect(formatDateTime(null)).toBe('n/a');
  });

  it('formats relative times', () => {
    const now = Date.parse('2026-01-15T10:00:00Z');
    expect(formatRelativeTime('2026-01-15T09:55:00Z', now, 'en')).toBe('5 minutes ago');
    expect(formatRelativeTime('2026-01-15T12:00:00Z', now, 'en')).toBe('in 2 hours');
    expect(formatRelativeTime('2026-01-15T10:00:00Z', now, 'en')).toBe('now');
    expect(formatRelativeTime('2026-01-12T10:00:00Z', now, 'en')).toBe('3 days ago');
  });
});

describe('formatCount', () => {
  it('formats a count in the locale and never invents a value', () => {
    expect(formatCount(12345)).toBe((12345).toLocaleString());
    expect(formatCount(0)).toBe('0');
    expect(formatCount(null)).toBe(NOT_AVAILABLE);
    expect(formatCount(Number.NaN)).toBe(NOT_AVAILABLE);
  });
});
