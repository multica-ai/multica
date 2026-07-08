import { describe, expect, it } from "vitest";
import type { AgentRuntime, RuntimeProfile } from "@multica/core/types";
import {
  PENDING_RUNTIME_WARNING_MS,
  isPendingCustomRuntime,
  isPendingCustomRuntimeWarning,
  pendingRuntimeCommandName,
  pendingRuntimeFromProfile,
  pendingRuntimeId,
  pendingRuntimesForProfiles,
} from "./pending-runtime";

function profile(overrides: Partial<RuntimeProfile> = {}): RuntimeProfile {
  return {
    id: "profile-1",
    workspace_id: "ws-1",
    display_name: "Team Codex",
    protocol_family: "codex",
    command_name: "team-codex",
    description: null,
    fixed_args: [],
    visibility: "workspace",
    created_by: "user-1",
    enabled: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    ...overrides,
  };
}

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Codex (MacBook)",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "codex",
    status: "online",
    device_info: "MacBook",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    profile_id: null,
    last_seen_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("pending custom runtime rows", () => {
  it("builds a pending runtime from the newly created profile", () => {
    const createdAt = Date.parse("2026-01-01T00:00:00Z");
    const pending = pendingRuntimeFromProfile({
      profile: profile(),
      createdAt,
      ownerId: "user-1",
      localDaemonId: "daemon-1",
      localMachineName: "MacBook",
    });

    expect(pending.id).toBe(pendingRuntimeId("profile-1"));
    expect(pending.name).toBe("Team Codex (MacBook)");
    expect(pending.daemon_id).toBe("daemon-1");
    expect(pending.profile_id).toBe("profile-1");
    expect(pending.provider).toBe("codex");
    expect(isPendingCustomRuntime(pending)).toBe(true);
    expect(pendingRuntimeCommandName(pending)).toBe("team-codex");
  });

  it("drops the pending row once a real runtime registers for the profile", () => {
    const createdAt = Date.parse("2026-01-01T00:00:00Z");
    const prof = profile();
    const baseRuntime = runtime();
    const registeredRuntime = runtime({
      id: "runtime-custom",
      profile_id: prof.id,
    });

    expect(
      pendingRuntimesForProfiles({
        pendingProfiles: [{ profile: prof, createdAt }],
        runtimes: [baseRuntime],
        ownerId: "user-1",
        localDaemonId: "daemon-1",
        localMachineName: "MacBook",
      }).map((item) => item.id),
    ).toEqual(["runtime-1", pendingRuntimeId(prof.id)]);

    expect(
      pendingRuntimesForProfiles({
        pendingProfiles: [{ profile: prof, createdAt }],
        runtimes: [baseRuntime, registeredRuntime],
        ownerId: "user-1",
        localDaemonId: "daemon-1",
        localMachineName: "MacBook",
      }).map((item) => item.id),
    ).toEqual(["runtime-1", "runtime-custom"]);
  });

  it("marks pending runtimes as waiting after the grace window", () => {
    const createdAt = Date.parse("2026-01-01T00:00:00Z");
    const pending = pendingRuntimeFromProfile({
      profile: profile(),
      createdAt,
    });

    expect(
      isPendingCustomRuntimeWarning(
        pending,
        createdAt + PENDING_RUNTIME_WARNING_MS - 1,
      ),
    ).toBe(false);
    expect(
      isPendingCustomRuntimeWarning(
        pending,
        createdAt + PENDING_RUNTIME_WARNING_MS,
      ),
    ).toBe(true);
  });
});
