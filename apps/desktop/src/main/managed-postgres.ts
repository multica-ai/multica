import { app } from "electron";
import { execFile } from "child_process";
import { existsSync } from "fs";
import { join } from "path";

// ManagedPostgres owns the lifecycle of an embedded Postgres cluster used by
// the local-only desktop product. The class is intentionally split from the
// actual binary invocation: a `PostgresRunner` interface lets unit tests drive
// the lifecycle with fakes, and the default runner shells out to the bundled
// initdb / pg_ctl / psql binaries resolved via `getPostgresBinaryPath()`.
//
// We never expose the cluster to the network — pg_ctl binds 127.0.0.1, and
// the connection string fixes the same loopback host. The credentials below
// match the existing local dev convention (multica:multica@.../multica).

export type ManagedPostgresOptions = {
  /** Directory where the `data/` cluster lives (Electron app userData/postgres). */
  dataDir: string;
  /** Loopback bind. We never expose Postgres to the network. */
  port: number;
  /** Command runners — injected for tests. */
  runner?: PostgresRunner;
  /** Stable timestamp source; injectable for tests. */
  now?: () => number;
  /** Number of health-check polls before giving up. Default 10. */
  healthCheckAttempts?: number;
  /** Delay between health-check polls in ms. Default 200. */
  pollIntervalMs?: number;
};

export interface PostgresRunner {
  /** initdb -D <dataDir> --auth-local=trust --username=multica */
  initdb: (dataDir: string) => Promise<void>;
  /** pg_ctl start -D <dataDir> -o "-p <port> -h 127.0.0.1" -w */
  start: (dataDir: string, port: number) => Promise<void>;
  /** pg_ctl stop -D <dataDir> -m fast -w */
  stop: (dataDir: string) => Promise<void>;
  /** psql -h 127.0.0.1 -p <port> -U multica -d postgres -c "SELECT 1" — return true on success. */
  healthCheck: (port: number) => Promise<boolean>;
  /** Determine whether dataDir is already initialised (PG_VERSION file exists). */
  isInitialised: (dataDir: string) => Promise<boolean>;
}

const DEFAULT_HEALTH_CHECK_ATTEMPTS = 10;
const DEFAULT_POLL_INTERVAL_MS = 200;

const BUNDLED_USER = "multica";
const BUNDLED_PASSWORD = "multica";
const BUNDLED_DB = "multica";

export class ManagedPostgres {
  private readonly dataDir: string;
  private readonly port: number;
  private readonly runner: PostgresRunner;
  private readonly healthCheckAttempts: number;
  private readonly pollIntervalMs: number;

  private running = false;

  constructor(opts: ManagedPostgresOptions) {
    this.dataDir = opts.dataDir;
    this.port = opts.port;
    this.runner = opts.runner ?? createDefaultPostgresRunner();
    this.healthCheckAttempts =
      opts.healthCheckAttempts ?? DEFAULT_HEALTH_CHECK_ATTEMPTS;
    this.pollIntervalMs = opts.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
  }

  /** Idempotent: if not initialised, run initdb; then start; then verify health. */
  async start(): Promise<void> {
    const initialised = await this.runner.isInitialised(this.dataDir);
    if (!initialised) {
      await this.runner.initdb(this.dataDir);
    }

    try {
      await this.runner.start(this.dataDir, this.port);
    } catch (err) {
      this.running = false;
      throw err;
    }

    const healthy = await this.pollHealth();
    if (!healthy) {
      this.running = false;
      throw new Error("Managed Postgres failed health check after start");
    }

    this.running = true;
  }

  /** No-op when already stopped. Best-effort — swallow errors. */
  async stop(): Promise<void> {
    if (!this.running) return;
    try {
      await this.runner.stop(this.dataDir);
    } catch (err) {
      // eslint-disable-next-line no-console
      console.error("[managed-postgres] stop failed", err);
    } finally {
      this.running = false;
    }
  }

  /** Sync flag mirroring the last successful start/stop transition. */
  isRunning(): boolean {
    return this.running;
  }

  /** Resolve the connection string a Go server / sqlc client would use. */
  getConnectionString(): string {
    return `postgres://${BUNDLED_USER}:${BUNDLED_PASSWORD}@127.0.0.1:${this.port}/${BUNDLED_DB}?sslmode=disable`;
  }

  // ---------------------------------------------------------------------

  private async pollHealth(): Promise<boolean> {
    for (let i = 0; i < this.healthCheckAttempts; i++) {
      const ok = await this.runner.healthCheck(this.port);
      if (ok) return true;
      if (i < this.healthCheckAttempts - 1 && this.pollIntervalMs > 0) {
        await delay(this.pollIntervalMs);
      }
    }
    return false;
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ---------------------------------------------------------------------
// Default runner
//
// Shells out to the bundled Postgres binaries. Until binary packaging lands
// (Task 11 follow-up — see scripts/package.mjs TODO), `getPostgresBinaryPath`
// returns null and every call rejects with a clear "not bundled" message so
// the supervisor surfaces it in the status panel instead of silently hanging.

const POSTGRES_BIN_DIRNAME = "postgres";

/**
 * Returns the absolute path to a bundled Postgres binary if it can be found
 * under the user-data Postgres directory. Returns null when the binary has
 * not yet been bundled — the caller is expected to translate that into a
 * descriptive error.
 */
export function getPostgresBinaryPath(name: string): string | null {
  let userData: string;
  try {
    userData = app.getPath("userData");
  } catch {
    // app.getPath throws before app `whenReady` resolves in some environments
    // (and during unit tests when electron is mocked). Treat it as "no path
    // available" — same effect as the binary not being bundled yet.
    return null;
  }
  const candidate = join(userData, POSTGRES_BIN_DIRNAME, "bin", name);
  if (existsSync(candidate)) return candidate;
  return null;
}

function runBinary(name: string, args: string[]): Promise<void> {
  const bin = getPostgresBinaryPath(name);
  if (!bin) {
    return Promise.reject(
      new Error(
        `postgres binaries not yet bundled (${name}) — run dev mode with MULTICA_USE_EXTERNAL_DB=1`,
      ),
    );
  }
  return new Promise((resolve, reject) => {
    execFile(bin, args, (err) => (err ? reject(err) : resolve()));
  });
}

export function createDefaultPostgresRunner(): PostgresRunner {
  return {
    initdb: async (dataDir) => {
      await runBinary("initdb", [
        "-D",
        dataDir,
        "--auth-local=trust",
        "--username=multica",
      ]);
    },
    start: async (dataDir, port) => {
      await runBinary("pg_ctl", [
        "start",
        "-D",
        dataDir,
        "-o",
        `-p ${port} -h 127.0.0.1`,
        "-w",
      ]);
    },
    stop: async (dataDir) => {
      await runBinary("pg_ctl", ["stop", "-D", dataDir, "-m", "fast", "-w"]);
    },
    healthCheck: async (port) => {
      const bin = getPostgresBinaryPath("psql");
      if (!bin) return false;
      return new Promise((resolve) => {
        execFile(
          bin,
          [
            "-h",
            "127.0.0.1",
            "-p",
            String(port),
            "-U",
            "multica",
            "-d",
            "postgres",
            "-c",
            "SELECT 1",
          ],
          (err) => resolve(!err),
        );
      });
    },
    isInitialised: async (dataDir) => {
      return existsSync(join(dataDir, "PG_VERSION"));
    },
  };
}
