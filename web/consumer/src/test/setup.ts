import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';
import { removePasskeys } from './passkeys';

// Testing Library unmounts after each test only when the runner has globals; Vitest runs without.
afterEach(() => {
  cleanup();
  removePasskeys();
  window.location.hash = '';
});
