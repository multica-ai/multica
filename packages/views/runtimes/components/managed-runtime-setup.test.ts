import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import {
  isPendingManagedRuntime,
  managedRuntimeSetupPhase,
  managedRuntimeSetupVersion,
  pendingManagedRuntimeId,
  runtimesWithManagedRuntimeSetup,
  type ManagedRuntimeSetupStatus,
} from "./managed-runtime-setup";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (MacBook)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "claude",
    status: "online",
    device_info: "MacBook",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: "2026-08-04T00:00:00Z",
    created_at: "2026-08-04T00:00:00Z",
    updated_at: "2026-08-04T00:00:00Z",
    ...overrides,
  };
}

function setup(
  overrides: Partial<ManagedRuntimeSetupStatus> = {},
): ManagedRuntimeSetupStatus {
  return {
    provider: "pi",
    phase: "installing",
    startedAt: "2026-08-04T00:00:00Z",
    ...overrides,
  };
}

describe("managed runtime setup rows", () => {
  it("adds a local pending Pi row beside registered runtimes", () => {
    const result = runtimesWithManagedRuntimeSetup({
      runtimes: [runtime()],
      setup: setup({ version: "0.83.0", source: "managed" }),
      workspaceId: "ws-1",
      localDaemonId: "daemon-1",
      localMachineName: "MacBook",
    });

    expect(result.map((item) => item.id)).toEqual([
      "runtime-1",
      pendingManagedRuntimeId("pi"),
    ]);
    expect(result[1]?.name).toBe("Pi (MacBook)");
    expect(result[1]?.provider).toBe("pi");
    expect(isPendingManagedRuntime(result[1]!)).toBe(true);
    expect(managedRuntimeSetupPhase(result[1]!)).toBe("installing");
    expect(managedRuntimeSetupVersion(result[1]!)).toBe("0.83.0");
  });

  it("lets a registered built-in Pi replace the pending row", () => {
    const registeredPi = runtime({
      id: "runtime-pi",
      name: "Pi (MacBook)",
      provider: "pi",
    });

    expect(
      runtimesWithManagedRuntimeSetup({
        runtimes: [runtime(), registeredPi],
        setup: setup({ phase: "ready" }),
        workspaceId: "ws-1",
      }),
    ).toEqual([runtime(), registeredPi]);
  });

  it("does not confuse a custom Pi profile with the managed built-in", () => {
    const customPi = runtime({
      id: "runtime-custom-pi",
      name: "Custom Pi (MacBook)",
      provider: "pi",
      profile_id: "profile-pi",
    });

    const result = runtimesWithManagedRuntimeSetup({
      runtimes: [customPi],
      setup: setup({ phase: "failed" }),
      workspaceId: "ws-1",
    });

    expect(result).toHaveLength(2);
    expect(managedRuntimeSetupPhase(result[1]!)).toBe("failed");
  });

  it("uses the caller-provided machine fallback for pending rows", () => {
    const result = runtimesWithManagedRuntimeSetup({
      runtimes: [],
      setup: setup(),
      workspaceId: "ws-1",
      fallbackMachineName: "This device",
    });

    expect(result[0]?.name).toBe("Pi (This device)");
    expect(result[0]?.device_info).toBe("This device");
  });
});
