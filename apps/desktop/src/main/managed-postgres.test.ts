import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("electron", () => ({
  app: {
    getPath: vi.fn().mockReturnValue("/tmp/multica-test-userData"),
  },
}));

import { ManagedPostgres } from "./managed-postgres";
import type { PostgresRunner } from "./managed-postgres";

type RunnerOverrides = Partial<PostgresRunner>;

function makeRunner(overrides: RunnerOverrides = {}): {
  runner: PostgresRunner;
  initdb: ReturnType<typeof vi.fn>;
  start: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
  healthCheck: ReturnType<typeof vi.fn>;
  isInitialised: ReturnType<typeof vi.fn>;
} {
  const initdb = vi.fn(overrides.initdb ?? (async () => {}));
  const start = vi.fn(overrides.start ?? (async () => {}));
  const stop = vi.fn(overrides.stop ?? (async () => {}));
  const healthCheck = vi.fn(overrides.healthCheck ?? (async () => true));
  const isInitialised = vi.fn(
    overrides.isInitialised ?? (async () => false),
  );
  return {
    runner: { initdb, start, stop, healthCheck, isInitialised },
    initdb,
    start,
    stop,
    healthCheck,
    isInitialised,
  };
}

const DATA_DIR = "/tmp/managed-pg/data";
const PORT = 55432;

describe("ManagedPostgres", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("first start initialises the cluster, then starts and reports running", async () => {
    const r = makeRunner({ isInitialised: async () => false });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    await pg.start();

    expect(r.initdb).toHaveBeenCalledTimes(1);
    expect(r.initdb).toHaveBeenCalledWith(DATA_DIR);
    expect(r.start).toHaveBeenCalledTimes(1);
    expect(r.start).toHaveBeenCalledWith(DATA_DIR, PORT);
    expect(pg.isRunning()).toBe(true);
  });

  it("subsequent start skips initdb when the cluster is already initialised", async () => {
    const r = makeRunner({ isInitialised: async () => true });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    await pg.start();

    expect(r.initdb).not.toHaveBeenCalled();
    expect(r.start).toHaveBeenCalledTimes(1);
    expect(pg.isRunning()).toBe(true);
  });

  it("rejects with a clear error and stays not-running when health check never succeeds", async () => {
    const r = makeRunner({
      isInitialised: async () => true,
      healthCheck: async () => false,
    });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
      healthCheckAttempts: 5,
    });

    await expect(pg.start()).rejects.toThrow(
      "Managed Postgres failed health check after start",
    );
    expect(pg.isRunning()).toBe(false);
    expect(r.healthCheck).toHaveBeenCalledTimes(5);
  });

  it("succeeds when health check returns false a few times then true", async () => {
    let attempts = 0;
    const r = makeRunner({
      isInitialised: async () => true,
      healthCheck: async () => {
        attempts += 1;
        return attempts >= 4;
      },
    });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
      healthCheckAttempts: 10,
    });

    await pg.start();

    expect(r.healthCheck).toHaveBeenCalledTimes(4);
    expect(pg.isRunning()).toBe(true);
  });

  it("stop is idempotent — second call is a no-op when already stopped", async () => {
    const r = makeRunner({ isInitialised: async () => true });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    await pg.start();
    expect(pg.isRunning()).toBe(true);

    await pg.stop();
    await pg.stop();

    expect(r.stop).toHaveBeenCalledTimes(1);
    expect(pg.isRunning()).toBe(false);
  });

  it("stop swallows runner errors and still marks not-running", async () => {
    const r = makeRunner({
      isInitialised: async () => true,
      stop: async () => {
        throw new Error("pg_ctl exit 1");
      },
    });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    // Suppress the expected console.error during this assertion.
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    await pg.start();
    await expect(pg.stop()).resolves.toBeUndefined();
    expect(pg.isRunning()).toBe(false);

    errSpy.mockRestore();
  });

  it("health check is called with the configured port", async () => {
    const r = makeRunner({ isInitialised: async () => true });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    await pg.start();

    expect(r.healthCheck).toHaveBeenCalledWith(PORT);
  });

  it("connection string uses the configured port and the standard local credentials", () => {
    const r = makeRunner();
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: 65432,
      runner: r.runner,
    });

    expect(pg.getConnectionString()).toBe(
      "postgres://multica:multica@127.0.0.1:65432/multica?sslmode=disable",
    );
  });

  it("isRunning() is false when the underlying start throws", async () => {
    const r = makeRunner({
      isInitialised: async () => true,
      start: async () => {
        throw new Error("port bound");
      },
    });
    const pg = new ManagedPostgres({
      dataDir: DATA_DIR,
      port: PORT,
      runner: r.runner,
      pollIntervalMs: 0,
    });

    await expect(pg.start()).rejects.toThrow("port bound");
    expect(pg.isRunning()).toBe(false);
    expect(r.healthCheck).not.toHaveBeenCalled();
  });
});
