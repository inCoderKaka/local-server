import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';

// Testing Library unmounts after each test only when the runner has globals; Vitest runs without.
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  window.location.hash = '';
});

// jsdom has no canvas; say so quietly (its default logs "not implemented") so the chart takes
// its table fallback, as a browser without canvas would.
HTMLCanvasElement.prototype.getContext = (() => null) as typeof HTMLCanvasElement.prototype.getContext;
