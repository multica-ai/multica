import { describe, expect, it, vi } from "vitest";

import type { LocalDataPaths } from "./local-data-paths";
import { performLocalReset, type ResetRunner } from "./local-reset";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makePaths(overrides: Partial<LocalDataPaths> = {}): LocalDataPaths {
  const root = "/tmp/multica-user-data";
  return {
    root,
    postgresData: `${root}/postgres/data`,
    postgresLogs: `${root}/postgres/logs`,
    daemonLogs: `${root}/daemon/logs`,
    appLogs: `${root}/logs`,
    appConfig: root,
    ...overrides,
  };
}

function makeRunner(overrides: Partial<ResetRunner> = {}): ResetRunner {
  return {
    stopStack: vi.fn(async () => {}),
    removeDir: vi.fn(async () => {}),
    clearDaemonToken: vi.fn(async () => {}),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("performLocalReset", () => {
  it("happy path: removes all four resettable paths under app-owned root", async () => {
    const paths = makePaths();
    const runner = makeRunner();

    const result = await performLocalReset({ paths, runner });

    expect(result.ok).toBe(true);
    expect(result.errors).toEqual([]);
    expect(result.skipped).toEqual([]);
    expect(result.removed).toEqual([
      paths.postgresData,
      paths.postgresLogs,
      paths.daemonLogs,
      paths.appLogs,
    ]);
    // appLogs lives under the userData root in this fixture (linux-style
    // layout). All four should have been targeted.
    expect(runner.removeDir).toHaveBeenCalledTimes(4);
    expect(runner.removeDir).toHaveBeenCalledWith(paths.postgresData);
    expect(runner.removeDir).toHaveBeenCalledWith(paths.postgresLogs);
    expect(runner.removeDir).toHaveBeenCalledWith(paths.daemonLogs);
    expect(runner.removeDir).toHaveBeenCalledWith(paths.appLogs);
  });

  it("skips a path that resolves outside the app-owned root", async () => {
    // Simulate a mutation/spoof: the daemonLogs key now points at /etc.
    const paths = makePaths({ daemonLogs: "/etc" });
    const runner = makeRunner();

    const result = await performLocalReset({ paths, runner });

    expect(runner.removeDir).not.toHaveBeenCalledWith("/etc");
    expect(result.skipped).toContain("/etc");
    expect(result.removed).not.toContain("/etc");
    // Other paths still removed.
    expect(result.removed).toContain(paths.postgresData);
  });

  it("continues despite individual rm failures and records them", async () => {
    const paths = makePaths();
    const removeDir = vi.fn(async (target: string) => {
      if (target === paths.postgresData) {
        throw new Error("EBUSY: locked");
      }
    });
    const runner = makeRunner({ removeDir });

    const result = await performLocalReset({ paths, runner });

    expect(result.ok).toBe(false);
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0]?.path).toBe(paths.postgresData);
    expect(result.errors[0]?.error).toContain("EBUSY");
    // Remaining paths still got removed.
    expect(result.removed).toContain(paths.postgresLogs);
    expect(result.removed).toContain(paths.daemonLogs);
    expect(result.removed).toContain(paths.appLogs);
  });

  it("records — but does not abort on — a stopStack failure", async () => {
    const paths = makePaths();
    const stopStack = vi.fn(async () => {
      throw new Error("supervisor wedged");
    });
    const runner = makeRunner({ stopStack });

    const result = await performLocalReset({ paths, runner });

    expect(result.errors.some((e) => e.path === "stopStack")).toBe(true);
    // Reset still proceeded with deletions.
    expect(runner.removeDir).toHaveBeenCalledTimes(4);
    expect(result.removed.length).toBe(4);
  });

  it("never touches appConfig or root, even though they exist on the paths shape", async () => {
    const paths = makePaths();
    const runner = makeRunner();

    await performLocalReset({ paths, runner });

    expect(runner.removeDir).not.toHaveBeenCalledWith(paths.appConfig);
    expect(runner.removeDir).not.toHaveBeenCalledWith(paths.root);
  });

  it("calls clearDaemonToken once", async () => {
    const paths = makePaths();
    const runner = makeRunner();

    await performLocalReset({ paths, runner });

    expect(runner.clearDaemonToken).toHaveBeenCalledTimes(1);
  });

  it("stops the stack BEFORE any removeDir call", async () => {
    const paths = makePaths();
    const callOrder: string[] = [];
    const runner = makeRunner({
      stopStack: vi.fn(async () => {
        callOrder.push("stopStack");
      }),
      removeDir: vi.fn(async (target: string) => {
        callOrder.push(`removeDir:${target}`);
      }),
      clearDaemonToken: vi.fn(async () => {
        callOrder.push("clearDaemonToken");
      }),
    });

    await performLocalReset({ paths, runner });

    const stopIdx = callOrder.indexOf("stopStack");
    const firstRm = callOrder.findIndex((s) => s.startsWith("removeDir:"));
    expect(stopIdx).toBeGreaterThanOrEqual(0);
    expect(firstRm).toBeGreaterThan(stopIdx);
  });

  it("logs through the injected logger when provided", async () => {
    const paths = makePaths();
    const info = vi.fn();
    const error = vi.fn();
    const runner = makeRunner({
      removeDir: vi.fn(async (target: string) => {
        if (target === paths.postgresData) throw new Error("boom");
      }),
    });

    await performLocalReset({
      paths,
      runner,
      logger: { info, error },
    });

    expect(info).toHaveBeenCalled();
    expect(error).toHaveBeenCalled();
  });
});
