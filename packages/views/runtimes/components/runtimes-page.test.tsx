import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import { filterRuntimesByScope } from "./runtimes-page";

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (dev-machine.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev-machine.local · claude 1.0.0",
    metadata: { cli_version: "0.3.0" },
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-05-17T11:59:50Z",
    created_at: "2026-05-17T11:00:00Z",
    updated_at: "2026-05-17T11:00:00Z",
    ...overrides,
  };
}

describe("runtime scope filtering", () => {
  it("keeps only the current user's runtimes in mine scope", () => {
    const runtimes = [
      makeRuntime({ id: "mine", owner_id: "user-1" }),
      makeRuntime({ id: "other", owner_id: "user-2", daemon_id: "daemon-2" }),
    ];

    expect(filterRuntimesByScope(runtimes, "mine", "user-1")).toEqual([
      expect.objectContaining({ id: "mine" }),
    ]);
  });

  it("shows all runtimes in all scope", () => {
    const runtimes = [
      makeRuntime({ id: "mine", owner_id: "user-1" }),
      makeRuntime({ id: "other", owner_id: "user-2", daemon_id: "daemon-2" }),
    ];

    expect(filterRuntimesByScope(runtimes, "all", "user-1")).toHaveLength(2);
  });

  it("returns no runtimes for mine scope before auth is ready", () => {
    const runtimes = [makeRuntime({ id: "mine", owner_id: "user-1" })];

    expect(filterRuntimesByScope(runtimes, "mine", null)).toEqual([]);
  });
});
