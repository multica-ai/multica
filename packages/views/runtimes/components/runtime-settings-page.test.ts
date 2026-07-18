import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import { resolveRuntimeSettingsTarget } from "./runtime-settings-page";

function runtime(
  id: string,
  daemonId: string,
  provider: string,
): AgentRuntime {
  return {
    id,
    workspace_id: "ws-1",
    daemon_id: daemonId,
    name: `${provider} (${daemonId})`,
    runtime_mode: "local",
    provider,
    launch_header: provider,
    status: "online",
    device_info: daemonId,
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("resolveRuntimeSettingsTarget", () => {
  const machineARuntime = runtime("runtime-a", "daemon-a", "codex");
  const machineBRuntime = runtime("runtime-b", "daemon-b", "claude");
  const runtimes = [machineARuntime, machineBRuntime];

  it("resolves a runtime inside its owning machine", () => {
    const target = resolveRuntimeSettingsTarget(
      runtimes,
      "local:daemon-a",
      "runtime-a",
    );

    expect(target?.runtime.id).toBe("runtime-a");
    expect(target?.machine.id).toBe("local:daemon-a");
  });

  it("rejects a runtime that belongs to a different machine", () => {
    expect(
      resolveRuntimeSettingsTarget(
        runtimes,
        "local:daemon-a",
        "runtime-b",
      ),
    ).toBeNull();
  });
});
