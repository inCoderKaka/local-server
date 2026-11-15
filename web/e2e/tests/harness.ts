import { spawn, type ChildProcess } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test as base } from '@playwright/test';

/**
 * Starts the Go harness TestE2EServer (cmd/local-server/e2e_harness_test.go) once per worker:
 * the real `serve --engine fake`, with an in-memory keychain, a fake terminal and a fake ready
 * Tailscale whose listeners are redirected to loopback, on free loopback ports and a throwaway
 * data directory that is removed afterwards. Each project of playwright.config.ts runs in its own
 * worker, so each spec gets a fresh host. The harness is a compiled test binary (`make web-e2e`
 * builds it), so SIGTERM reaches it.
 */

/** How often the ready file is looked for while the harness starts. */
const READY_POLL_MS = 100;
/** Longest the harness may take to listen. */
const READY_TIMEOUT_MS = 30_000;
/** Longest the harness may take to stop after SIGTERM before it is killed. */
const STOP_TIMEOUT_MS = 20_000;

const here = fileURLToPath(new URL('.', import.meta.url));
/** The harness binary: LS_E2E_HARNESS, else bin/e2e-harness at the repository root. */
const harnessPath = process.env.LS_E2E_HARNESS ?? resolve(here, '../../../bin/e2e-harness');

export interface Server {
  /** The admin listener, for example http://127.0.0.1:53123. */
  admin: string;
  /** The consumer API listener. */
  consumer: string;
  /**
   * The consumer page listener, for example https://127.0.0.1:53125. It serves TLS only, with the
   * harness's self-signed test certificate, so a browser context for it sets ignoreHTTPSErrors.
   */
  page: string;
}

function exited(child: ChildProcess): Promise<number | null> {
  return new Promise((done) => {
    if (child.exitCode !== null || child.signalCode !== null) done(child.exitCode);
    else child.once('exit', (code) => done(code));
  });
}

async function startHarness(): Promise<{ server: Server; stop: () => Promise<void> }> {
  if (!existsSync(harnessPath)) throw new Error(`no harness binary at ${harnessPath}: run make web-e2e`);
  const tmp = await mkdtemp(join(tmpdir(), 'ls-e2e-'));
  const readyFile = join(tmp, 'ready.json');
  // The harness prints only its own log lines (listener addresses, keychain call counts), never
  // serve's stderr, which holds the first run's recovery code.
  const child = spawn(harnessPath, ['-test.run', '^TestE2EServer$', '-test.timeout', '0', '-test.v'], {
    cwd: tmp,
    env: { ...process.env, LS_E2E_READY_FILE: readyFile, LS_E2E_DATA_DIR: join(tmp, 'data') },
    stdio: ['ignore', 'inherit', 'inherit'],
  });
  const exit = exited(child);

  const stop = async () => {
    if (child.exitCode === null && child.signalCode === null) {
      child.kill('SIGTERM');
      const timer = setTimeout(() => child.kill('SIGKILL'), STOP_TIMEOUT_MS);
      const code = await exit;
      clearTimeout(timer);
      if (code !== 0) console.error(`harness exited with ${code}`);
    }
    await rm(tmp, { recursive: true, force: true });
  };

  const deadline = Date.now() + READY_TIMEOUT_MS;
  for (;;) {
    if (child.exitCode !== null || child.signalCode !== null) {
      await stop();
      throw new Error(`the harness exited with ${child.exitCode ?? child.signalCode} before it listened`);
    }
    if (existsSync(readyFile)) {
      const server = JSON.parse(await readFile(readyFile, 'utf8')) as Server;
      return { server, stop };
    }
    if (Date.now() > deadline) {
      await stop();
      throw new Error(`the harness did not listen within ${READY_TIMEOUT_MS} ms`);
    }
    await new Promise((r) => setTimeout(r, READY_POLL_MS));
  }
}

/** The test with a `server` worker fixture; baseURL is its admin listener. */
export const test = base.extend<object, { server: Server }>({
  server: [
    async ({}, use) => {
      const h = await startHarness();
      try {
        await use(h.server);
      } finally {
        await h.stop();
      }
    },
    { scope: 'worker' },
  ],
  baseURL: async ({ server }, use) => {
    await use(server.admin);
  },
});

export { expect } from '@playwright/test';
