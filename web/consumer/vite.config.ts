import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { searchForWorkspaceRoot } from 'vite';
import { defineConfig } from 'vitest/config';

const repo = (p: string) => new URL(`../../${p}`, import.meta.url).pathname;

// The built page is served under a strict CSP (internal/webui): no inline script or style, one
// stylesheet, nothing from another origin.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    fs: {
      // Beside the web workspaces, the page reads the legal texts (src/lib/legal.ts) and its test
      // reads the version ids the host requires. Nothing else outside web/ is readable.
      allow: [searchForWorkspaceRoot(process.cwd()), repo('docs/legal'), repo('internal/consumerauth')],
    },
  },
  build: {
    outDir: 'dist',
    // dist/.gitkeep is committed so `go build` works without a page build; never delete it. The
    // Makefile removes the old build output except that file.
    emptyOutDir: false,
    assetsDir: 'assets',
    // One stylesheet for the whole page.
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
