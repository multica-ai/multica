// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

import {
  DaemonOperationGate,
  DaemonRecoveryPolicy,
  RECOVERY_ATTEMPT_BUDGET,
  RECOVERY_ATTEMPT_WINDOW_MS,
  RECOVERY_BACKOFF_MS,
  RECOVERY_PID_DEFERRAL_LIMIT,
  RECOVERY_PID_RECHECK_MS,
  RECOVERY_STABLE_RUNNING_MS,
  daemonProcessExists,
  parseDaemonPid,
  recoveryStartAllowed,
  runDaemonRecoveryAttempt,
} from "./daemon-recovery";

const eligible = {
  desiredRunning: true,
  externalDaemonObserved: false,
  lifecycleBusy: false,
  state: "stopped" as const,
  now: 1_000,
};

function reachDecision(
  policy: DaemonRecoveryPolicy,
  now: number,
): ReturnType<DaemonRecoveryPolicy["observe"]> {
  policy.observe({ ...eligible, now });
  policy.observe({ ...eligible, now });
  return policy.observe({ ...eligible, now });
}

describe("DaemonRecoveryPolicy", () => {
  it("requires three consecutive stopped polls before confirmation", () => {
    const policy = new DaemonRecoveryPolicy();
    expect(policy.observe(eligible)).toBe("none");
    expect(policy.observe(eligible)).toBe("none");
    expect(policy.observe(eligible)).toBe("confirm");
  });

  it.each([
    { desiredRunning: false },
    { externalDaemonObserved: true },
    { lifecycleBusy: true },
    { state: "running" as const },
    { state: "starting" as const },
    { state: "stopping" as const },
    { state: "installing_cli" as const },
    { state: "cli_not_found" as const },
    { state: "auth_expired" as const },
    { state: "recovery_paused" as const },
  ])("does not recover when ineligible: %o", (override) => {
    const policy = new DaemonRecoveryPolicy();
    for (let i = 0; i < 5; i += 1) {
      expect(policy.observe({ ...eligible, ...override })).toBe("none");
    }
  });

  it("keeps escalating backoff through a short-lived running crash loop", () => {
    const policy = new DaemonRecoveryPolicy();
    expect(reachDecision(policy, 1_000)).toBe("confirm");
    policy.recordRecoveryAttempt(1_000);

    policy.observe({ ...eligible, state: "starting", now: 2_000 });
    policy.observe({ ...eligible, state: "running", now: 5_000 });
    expect(reachDecision(policy, 20_000)).toBe("confirm");
    policy.recordRecoveryAttempt(20_000);

    policy.observe({ ...eligible, state: "running", now: 25_000 });
    expect(reachDecision(policy, 20_000 + RECOVERY_BACKOFF_MS[1] - 1)).toBe(
      "none",
    );
    expect(reachDecision(policy, 20_000 + RECOVERY_BACKOFF_MS[1])).toBe(
      "confirm",
    );
  });

  it("forgives the backoff only after sustained running", () => {
    const policy = new DaemonRecoveryPolicy();
    policy.recordRecoveryAttempt(1_000);
    policy.recordRecoveryAttempt(2_000);
    policy.observe({ ...eligible, state: "running", now: 3_000 });
    policy.observe({
      ...eligible,
      state: "running",
      now: 3_000 + RECOVERY_STABLE_RUNNING_MS,
    });

    expect(reachDecision(policy, 64_000)).toBe("confirm");
    policy.recordRecoveryAttempt(64_000);
    expect(reachDecision(policy, 64_000 + RECOVERY_BACKOFF_MS[0] - 1)).toBe(
      "none",
    );
    expect(reachDecision(policy, 64_000 + RECOVERY_BACKOFF_MS[0])).toBe(
      "confirm",
    );
  });

  it("pauses visibly after the absolute attempt budget", () => {
    const policy = new DaemonRecoveryPolicy();
    for (let i = 0; i < RECOVERY_ATTEMPT_BUDGET; i += 1) {
      policy.recordRecoveryAttempt(1_000 + i);
    }
    expect(
      reachDecision(
        policy,
        1_000 + RECOVERY_ATTEMPT_BUDGET + RECOVERY_BACKOFF_MS.at(-1)!,
      ),
    ).toBe("pause");
    expect(policy.isPaused).toBe(true);
    expect(policy.observe(eligible)).toBe("pause");

    policy.reset();
    expect(policy.isPaused).toBe(false);
    expect(reachDecision(policy, 2_000)).toBe("confirm");
  });

  it("expires old attempts from the rolling budget", () => {
    const policy = new DaemonRecoveryPolicy();
    for (let i = 0; i < RECOVERY_ATTEMPT_BUDGET; i += 1) {
      policy.recordRecoveryAttempt(1_000 + i);
    }
    expect(
      reachDecision(policy, 1_000 + RECOVERY_ATTEMPT_WINDOW_MS + 1),
    ).toBe("confirm");
  });

  it("bounds a live or unverifiable PID to three deferrals", () => {
    const policy = new DaemonRecoveryPolicy();
    expect(policy.recordPidDeferral(1_000)).toBe(false);
    expect(reachDecision(policy, 1_000 + RECOVERY_PID_RECHECK_MS - 1)).toBe(
      "none",
    );
    expect(reachDecision(policy, 1_000 + RECOVERY_PID_RECHECK_MS)).toBe(
      "confirm",
    );

    policy.reset();
    for (let i = 1; i < RECOVERY_PID_DEFERRAL_LIMIT; i += 1) {
      expect(policy.recordPidDeferral(i * RECOVERY_PID_RECHECK_MS)).toBe(false);
    }
    expect(
      policy.recordPidDeferral(
        RECOVERY_PID_DEFERRAL_LIMIT * RECOVERY_PID_RECHECK_MS,
      ),
    ).toBe(true);
  });
});

describe("recovery orchestration guards", () => {
  const profile = { name: "desktop-api.multica.ai", port: 19_999 };

  it("allows first-start recovery for a Desktop-owned profile", () => {
    expect(
      recoveryStartAllowed({
        desiredRunning: true,
        externalDaemonObserved: false,
        expected: profile,
        current: profile,
      }),
    ).toBe(true);
  });

  it("recovers an initial timed-out start without prior healthy ownership", async () => {
    const start = vi.fn(async () => ({ success: true }));
    const outcome = await runDaemonRecoveryAttempt({
      startAllowed: () =>
        recoveryStartAllowed({
          desiredRunning: true,
          externalDaemonObserved: false,
          expected: profile,
          current: profile,
        }),
      confirmAlive: async () => false,
      pidConfirmedAbsent: async () => true,
      recordPidDeferral: () => false,
      recordPidAbsent: vi.fn(),
      onPidFallback: vi.fn(),
      start,
      desiredRunning: () => true,
      stop: vi.fn(async () => ({ success: true })),
    });

    expect(outcome).toEqual({ kind: "started" });
    expect(start).toHaveBeenCalledOnce();
  });

  it("does not start after the profile changes during confirmation", async () => {
    let current = profile;
    let finishConfirmation: (alive: boolean) => void = () => {};
    const confirmation = new Promise<boolean>((resolve) => {
      finishConfirmation = resolve;
    });
    const start = vi.fn(async () => ({ success: true }));
    const attempt = runDaemonRecoveryAttempt({
      startAllowed: () =>
        recoveryStartAllowed({
          desiredRunning: true,
          externalDaemonObserved: false,
          expected: profile,
          current,
        }),
      confirmAlive: () => confirmation,
      pidConfirmedAbsent: vi.fn(async () => true),
      recordPidDeferral: () => false,
      recordPidAbsent: vi.fn(),
      onPidFallback: vi.fn(),
      start,
      desiredRunning: () => true,
      stop: vi.fn(async () => ({ success: true })),
    });

    current = { ...profile, name: "desktop-other" };
    finishConfirmation(false);
    await expect(attempt).resolves.toEqual({ kind: "superseded" });
    expect(start).not.toHaveBeenCalled();
  });

  it("rolls back a successful start when Stop arrives in flight", async () => {
    let desiredRunning = true;
    let finishStart: (result: { success: boolean }) => void = () => {};
    let markStartEntered: () => void = () => {};
    const startResult = new Promise<{ success: boolean }>((resolve) => {
      finishStart = resolve;
    });
    const startEntered = new Promise<void>((resolve) => {
      markStartEntered = resolve;
    });
    const stop = vi.fn(async () => ({ success: true }));
    const attempt = runDaemonRecoveryAttempt({
      startAllowed: () => desiredRunning,
      confirmAlive: async () => false,
      pidConfirmedAbsent: async () => true,
      recordPidDeferral: () => false,
      recordPidAbsent: vi.fn(),
      onPidFallback: vi.fn(),
      start: () => {
        markStartEntered();
        return startResult;
      },
      desiredRunning: () => desiredRunning,
      stop,
    });

    await startEntered;
    desiredRunning = false;
    finishStart({ success: true });
    await expect(attempt).resolves.toEqual({ kind: "cancelled" });
    expect(stop).toHaveBeenCalledOnce();
  });

  it("defers on an unverifiable PID, then uses the bounded fallback", async () => {
    const start = vi.fn(async () => ({ success: true }));
    let fallbackAllowed = false;
    const dependencies = {
      startAllowed: () => true,
      confirmAlive: async () => false,
      pidConfirmedAbsent: async () => false,
      recordPidDeferral: () => fallbackAllowed,
      recordPidAbsent: vi.fn(),
      onPidFallback: vi.fn(),
      start,
      desiredRunning: () => true,
      stop: vi.fn(async () => ({ success: true })),
    };

    await expect(runDaemonRecoveryAttempt(dependencies)).resolves.toEqual({
      kind: "pid_deferred",
    });
    expect(start).not.toHaveBeenCalled();

    fallbackAllowed = true;
    await expect(runDaemonRecoveryAttempt(dependencies)).resolves.toEqual({
      kind: "started",
    });
    expect(dependencies.onPidFallback).toHaveBeenCalledOnce();
    expect(start).toHaveBeenCalledOnce();
  });

  it.each([
    { desiredRunning: false },
    { externalDaemonObserved: true },
    { current: null },
    { current: { ...profile, name: "desktop-other" } },
    { current: { ...profile, port: profile.port + 1 } },
  ])("rejects a superseded recovery: %o", (override) => {
    expect(
      recoveryStartAllowed({
        desiredRunning: true,
        externalDaemonObserved: false,
        expected: profile,
        current: profile,
        ...override,
      }),
    ).toBe(false);
  });

  it("queues a one-shot login intent behind bootstrap", async () => {
    const gate = new DaemonOperationGate();
    let releaseBackground: () => void = () => {};
    const background = gate.runBackground(
      () =>
        new Promise<string>((resolve) => {
          releaseBackground = () => resolve("recovered");
        }),
    );
    const foregroundFn = vi.fn(async () => "stopped");
    const foreground = gate.runForeground(foregroundFn);
    const retryablePollFn = vi.fn(async () => "polled");

    await Promise.resolve();
    expect(foregroundFn).not.toHaveBeenCalled();
    await expect(
      gate.runBackground(retryablePollFn),
    ).resolves.toMatchObject({ operationBusy: true });
    expect(retryablePollFn).not.toHaveBeenCalled();
    releaseBackground();
    await expect(background).resolves.toBe("recovered");
    await expect(foreground).resolves.toBe("stopped");
    expect(foregroundFn).toHaveBeenCalledOnce();
  });

  it("queues one-shot foreground intents in invocation order", async () => {
    const gate = new DaemonOperationGate();
    let releaseFirst: () => void = () => {};
    const first = gate.runForeground(
      () =>
        new Promise<string>((resolve) => {
          releaseFirst = () => resolve("first");
        }),
    );
    const secondFn = vi.fn(async () => "second");
    const second = gate.runForeground(secondFn);

    await Promise.resolve();
    expect(secondFn).not.toHaveBeenCalled();
    releaseFirst();
    await expect(first).resolves.toBe("first");
    await expect(second).resolves.toBe("second");
    expect(secondFn).toHaveBeenCalledOnce();
  });

  it("rejects foreground reentrancy instead of waiting on itself", async () => {
    const gate = new DaemonOperationGate();
    const nested = vi.fn(async () => "nested");

    await expect(
      gate.runForeground(() => gate.runForeground(nested)),
    ).rejects.toThrow("cannot be called from inside a gated operation");
    expect(nested).not.toHaveBeenCalled();
    expect(gate.inProgress).toBe(false);
    await expect(gate.runForeground(async () => "recovered")).resolves.toBe(
      "recovered",
    );
  });

  it("lets a queued member stop revoke recovery intent immediately", async () => {
    const gate = new DaemonOperationGate();
    let desiredRunning = true;
    let releaseProbe: () => void = () => {};
    const start = vi.fn(async () => ({ success: true }));
    const recovery = gate.runBackground(() =>
      runDaemonRecoveryAttempt({
        startAllowed: () => desiredRunning,
        confirmAlive: async () => {
          await new Promise<void>((resolve) => {
            releaseProbe = resolve;
          });
          return false;
        },
        pidConfirmedAbsent: async () => true,
        recordPidDeferral: () => false,
        recordPidAbsent: vi.fn(),
        onPidFallback: vi.fn(),
        start,
        desiredRunning: () => desiredRunning,
        stop: vi.fn(async () => ({ success: true })),
      }),
    );

    desiredRunning = false;
    const stop = gate.runForeground(async () => "stopped");
    releaseProbe();
    await expect(recovery).resolves.toEqual({ kind: "superseded" });
    await expect(stop).resolves.toBe("stopped");
    expect(start).not.toHaveBeenCalled();
  });

  it("does not queue new background work behind a member operation", async () => {
    const gate = new DaemonOperationGate();
    let releaseForeground: () => void = () => {};
    const foreground = gate.runForeground(
      () =>
        new Promise<string>((resolve) => {
          releaseForeground = () => resolve("started");
        }),
    );
    await Promise.resolve();

    await expect(
      gate.runBackground(async () => "background"),
    ).resolves.toMatchObject({ operationBusy: true });
    releaseForeground();
    await expect(foreground).resolves.toBe("started");
  });
});

describe("daemon PID checks", () => {
  it.each([
    ["42", 42],
    ["42\n", 42],
    ["", null],
    ["0", null],
    ["-1", null],
    ["12x", null],
  ])("parses %j as %j", (raw, expected) => {
    expect(parseDaemonPid(raw)).toBe(expected);
  });

  it("treats only ESRCH as proof that a PID is absent", () => {
    const alive = vi.fn();
    expect(daemonProcessExists(42, alive)).toBe(true);
    expect(alive).toHaveBeenCalledWith(42, 0);

    expect(
      daemonProcessExists(42, () => {
        throw Object.assign(new Error("gone"), { code: "ESRCH" });
      }),
    ).toBe(false);
    expect(
      daemonProcessExists(42, () => {
        throw Object.assign(new Error("denied"), { code: "EPERM" });
      }),
    ).toBe(true);
  });
});
