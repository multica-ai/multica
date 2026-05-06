import { describe, expect, it, vi, beforeEach } from "vitest";

import { LocalStackSupervisor } from "./local-stack-supervisor";
import type { LocalStackComponentRunner } from "./local-stack-supervisor";
import type {
  LocalStackComponentName,
  LocalStackStatus,
} from "../shared/local-stack-types";
import { LOCAL_STACK_COMPONENT_ORDER } from "../shared/local-stack-types";

type RunnerBehavior =
  | { kind: "resolve" }
  | { kind: "reject"; message: string }
  | { kind: "rejectThenResolve"; message: string };

interface FakeRunnerHandle {
  runner: LocalStackComponentRunner;
  start: ReturnType<typeof vi.fn>;
  stop: ReturnType<typeof vi.fn>;
}

function makeRunner(
  name: LocalStackComponentName,
  behavior: RunnerBehavior,
): FakeRunnerHandle {
  let attempts = 0;
  const start = vi.fn(async () => {
    attempts += 1;
    if (behavior.kind === "resolve") return;
    if (behavior.kind === "reject") {
      throw new Error(behavior.message);
    }
    if (behavior.kind === "rejectThenResolve") {
      if (attempts === 1) throw new Error(behavior.message);
      return;
    }
  });
  const stop = vi.fn(async () => {});
  return {
    runner: { name, start, stop },
    start,
    stop,
  };
}

function makeAllResolvingRunners(): FakeRunnerHandle[] {
  return LOCAL_STACK_COMPONENT_ORDER.map((name) =>
    makeRunner(name, { kind: "resolve" }),
  );
}

function makeFixedClock(): () => number {
  let t = 1_000;
  return () => {
    t += 1;
    return t;
  };
}

describe("LocalStackSupervisor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("marches through every component to ready and broadcasts each transition", async () => {
    const handles = makeAllResolvingRunners();
    const captured: LocalStackStatus[] = [];
    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      onStatusChange: (status) => captured.push(structuredClone(status)),
      now: makeFixedClock(),
    });

    await supervisor.start();

    const status = supervisor.getStatus();
    expect(status.overall).toBe("ready");
    for (const c of status.components) {
      expect(c.state).toBe("ready");
      expect(c.detail).toBeNull();
    }

    // We expect at minimum a "starting" + "ready" transition per component.
    expect(captured.length).toBeGreaterThanOrEqual(
      LOCAL_STACK_COMPONENT_ORDER.length * 2,
    );

    // Each component's start invoked exactly once.
    for (const handle of handles) {
      expect(handle.start).toHaveBeenCalledTimes(1);
    }
  });

  it("stops at the first failing component and marks the overall status failing", async () => {
    const handles: FakeRunnerHandle[] = [
      makeRunner("database", { kind: "reject", message: "port 5432 in use" }),
      makeRunner("migrations", { kind: "resolve" }),
      makeRunner("api", { kind: "resolve" }),
      makeRunner("bootstrap", { kind: "resolve" }),
      makeRunner("daemon", { kind: "resolve" }),
      makeRunner("runtimeRegistration", { kind: "resolve" }),
    ];

    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      now: makeFixedClock(),
    });

    await supervisor.start();

    const status = supervisor.getStatus();
    expect(status.overall).toBe("failing");

    const db = status.components.find((c) => c.name === "database")!;
    expect(db.state).toBe("failing");
    expect(db.detail).toBe("port 5432 in use");

    // No subsequent component should have been started or transitioned.
    expect(handles[1].start).not.toHaveBeenCalled();
    expect(handles[2].start).not.toHaveBeenCalled();
    for (let i = 1; i < handles.length; i++) {
      const c = status.components.find(
        (x) => x.name === handles[i].runner.name,
      )!;
      expect(c.state).toBe("pending");
      expect(c.detail).toBeNull();
    }
  });

  it("a migration failure halts API/bootstrap/daemon/runtimeRegistration", async () => {
    const handles: FakeRunnerHandle[] = [
      makeRunner("database", { kind: "resolve" }),
      makeRunner("migrations", {
        kind: "reject",
        message: "migration 0007 failed",
      }),
      makeRunner("api", { kind: "resolve" }),
      makeRunner("bootstrap", { kind: "resolve" }),
      makeRunner("daemon", { kind: "resolve" }),
      makeRunner("runtimeRegistration", { kind: "resolve" }),
    ];

    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      now: makeFixedClock(),
    });

    await supervisor.start();

    expect(supervisor.getStatus().overall).toBe("failing");
    expect(handles[0].start).toHaveBeenCalledTimes(1);
    expect(handles[1].start).toHaveBeenCalledTimes(1);
    // None of the downstream runners should have been touched.
    expect(handles[2].start).not.toHaveBeenCalled();
    expect(handles[3].start).not.toHaveBeenCalled();
    expect(handles[4].start).not.toHaveBeenCalled();
    expect(handles[5].start).not.toHaveBeenCalled();

    const migrations = supervisor
      .getStatus()
      .components.find((c) => c.name === "migrations")!;
    expect(migrations.state).toBe("failing");
    expect(migrations.detail).toBe("migration 0007 failed");
  });

  it("retry() re-invokes failed runners and proceeds when they recover", async () => {
    const handles: FakeRunnerHandle[] = [
      makeRunner("database", { kind: "resolve" }),
      makeRunner("migrations", {
        kind: "rejectThenResolve",
        message: "transient migration error",
      }),
      makeRunner("api", { kind: "resolve" }),
      makeRunner("bootstrap", { kind: "resolve" }),
      makeRunner("daemon", { kind: "resolve" }),
      makeRunner("runtimeRegistration", { kind: "resolve" }),
    ];

    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      now: makeFixedClock(),
    });

    await supervisor.start();
    expect(supervisor.getStatus().overall).toBe("failing");

    await supervisor.retry();

    const status = supervisor.getStatus();
    expect(status.overall).toBe("ready");

    // database already-ready: should not be re-invoked.
    expect(handles[0].start).toHaveBeenCalledTimes(1);
    // migrations: invoked twice (once failing, once recovering).
    expect(handles[1].start).toHaveBeenCalledTimes(2);
    // remaining runners run once after migrations recovers.
    expect(handles[2].start).toHaveBeenCalledTimes(1);
    expect(handles[3].start).toHaveBeenCalledTimes(1);
    expect(handles[4].start).toHaveBeenCalledTimes(1);
    expect(handles[5].start).toHaveBeenCalledTimes(1);
  });

  it("concurrent start() calls share the in-flight march without duplicating work", async () => {
    const handles = makeAllResolvingRunners();
    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      now: makeFixedClock(),
    });

    const a = supervisor.start();
    const b = supervisor.start();

    await Promise.all([a, b]);

    for (const handle of handles) {
      expect(handle.start).toHaveBeenCalledTimes(1);
    }
    expect(supervisor.getStatus().overall).toBe("ready");
  });

  it("stop() invokes runner.stop in reverse order", async () => {
    const handles = makeAllResolvingRunners();
    const callOrder: LocalStackComponentName[] = [];
    for (const handle of handles) {
      handle.stop.mockImplementation(async () => {
        callOrder.push(handle.runner.name);
      });
    }

    const supervisor = new LocalStackSupervisor({
      runners: handles.map((h) => h.runner),
      now: makeFixedClock(),
    });

    await supervisor.start();
    await supervisor.stop();

    for (const handle of handles) {
      expect(handle.stop).toHaveBeenCalledTimes(1);
    }
    expect(callOrder).toEqual(
      [...LOCAL_STACK_COMPONENT_ORDER].reverse(),
    );
  });
});
