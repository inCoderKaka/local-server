import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The built UI is served under a strict CSP (internal/webui): no inline script or style, one
// stylesheet, nothing from another origin.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    // dist/.gitkeep is committed so `go build` works without a UI build; never delete it. The
    // Makefile removes the old build output except that file.
    emptyOutDir: false,
    assetsDir: 'assets',
    // One stylesheet for the whole app, uPlot's included.
    cssCodeSplit: false,
    // No inlined assets: fonts or images as data: URLs would need a looser CSP.
    assetsInlineLimit: 0,
    // Every supported browser has modulepreload; the polyfill is not needed.
    modulePreload: { polyfill: false },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
  },
});
