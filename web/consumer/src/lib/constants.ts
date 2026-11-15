import { RANGES, type Granularity, type RangeChoice } from '@local-server/shared';

/**
 * The named display values of the consumer page, in one place (req_001 D-38). None of them is a
 * time, interval or limit of the host: every one of those comes from the API (the grant's quota
 * windows, the host's request expiry, the page size of a list). These only choose what the page
 * shows when the API leaves the choice to the reader.
 */

/**
 * The period the start page sums usage over when the grant has no quota window to count in: hour
 * buckets of the first hour range the usage pages offer (the last 24 hours).
 */
export const SUMMARY_PERIOD_WITHOUT_QUOTA: { granularity: Granularity; range: RangeChoice } = {
  granularity: 'hour',
  range: RANGES.hour[0]!,
};

/** The colours of the chart lines, one per model, in turn. */
export const SERIES_COLOURS = ['#0284c7', '#d97706', '#059669', '#7c3aed', '#dc2626', '#334155', '#db2777', '#65a30d'] as const;
