import { useEffect, useRef } from 'react';
import 'uplot/dist/uPlot.min.css';

/** One line of a Plot: its label, one value per x (null for a gap) and its colour. */
export type Series = { label: string; values: (number | null)[]; stroke: string };

/** Whether this browser can draw on a canvas (jsdom cannot). */
export function canDraw(): boolean {
  try {
    return !!document.createElement('canvas').getContext('2d');
  } catch {
    return false;
  }
}

/**
 * A uPlot line chart. uPlot sets its styles through the CSSOM (element.style), never through
 * style attributes or <style> elements, so it runs under style-src 'self'; its stylesheet is part
 * of the app's one stylesheet. It is loaded only when a canvas exists. uPlot writes series labels
 * with textContent, never as HTML (req_001 D-31). Pass stable `xs` and `series` (memoised), so
 * the chart redraws only when the data changes.
 */
export function Plot({ title, xs, series, format }: { title: string; xs: number[]; series: Series[]; format: (n: number | null) => string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el || !canDraw()) return;
    let plot: { destroy: () => void; setSize: (s: { width: number; height: number }) => void } | undefined;
    let observer: ResizeObserver | undefined;
    let cancelled = false;
    void import('uplot').then(({ default: UPlot }) => {
      if (cancelled) return;
      const height = Math.round(el.clientWidth / 3) || 200;
      const u = new UPlot(
        {
          title,
          width: el.clientWidth,
          height,
          series: [{}, ...series.map((s) => ({ label: s.label, stroke: s.stroke, width: 2, value: (_u: unknown, v: number | null) => format(v) }))],
          axes: [{}, { values: (_u: unknown, ticks: number[]) => ticks.map((t) => format(t)), size: 70 }],
        },
        [xs, ...series.map((s) => s.values)],
        el,
      );
      plot = u;
      observer = new ResizeObserver(() => u.setSize({ width: el.clientWidth, height: Math.round(el.clientWidth / 3) || height }));
      observer.observe(el);
    });
    return () => {
      cancelled = true;
      observer?.disconnect();
      plot?.destroy();
    };
  }, [title, xs, series, format]);
  return <div ref={ref} role="img" aria-label={`${title} over time`} />;
}
