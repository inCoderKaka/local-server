import { describe, expect, it } from 'vitest';
import { parseGoDuration } from './duration';

describe('parseGoDuration', () => {
  it.each([
    ['30s', 30_000],
    ['1m', 60_000],
    ['90s', 90_000],
    ['1h30m', 5_400_000],
    ['1.5h', 5_400_000],
    ['250ms', 250],
    ['2m3s', 123_000],
    ['0', 0],
    ['0s', 0],
    ['-5s', -5000],
    ['+5s', 5000],
    ['1000us', 1],
    ['1000µs', 1],
    ['1000000ns', 1],
    ['.5s', 500],
  ])('%s is %d ms', (s, ms) => {
    expect(parseGoDuration(s)).toBeCloseTo(ms, 9);
  });

  it.each(['', '30', 's', '1x', '1h 30m', 'soon', ' 30s', '30S', '-', '1..5s'])('%j does not parse', (s) => {
    expect(parseGoDuration(s)).toBeNull();
  });

  it('refuses what is not a string', () => {
    expect(parseGoDuration(30)).toBeNull();
    expect(parseGoDuration(null)).toBeNull();
    expect(parseGoDuration(undefined)).toBeNull();
  });
});
