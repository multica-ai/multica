import { describe, it, expect } from "vitest";
import type { RuntimeDevice } from "@multica/core/types";
import {
  buildAgentRuntimeMachines,
  isRuntimeUsableForUser,
} from "./runtime-picker";

function rt(overrides: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: "rt",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Claude (host.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "claude (stream-json)",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: "u1",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("buildAgentRuntimeMachines", () => {
  it("groups CLIs on the same daemon into one machine, sorted by provider", () => {
    const machines = buildAgentRuntimeMachines([
      rt({ id: "a", daemon_id: "d1", provider: "codex", name: "Codex (Mac)" }),
      rt({ id: "b", daemon_id: "d1", provider: "claude", name: "Claude (Mac)" }),
    ]);
    expect(machines).toHaveLength(1);
    expect(machines[0]!.runtimes.map((r) => r.provider)).toEqual([
      "claude",
      "codex",
    ]);
    expect(machines[0]!.label).toBe("Mac");
  });

  it("keeps two members' identically-named daemon-less hosts separate", () => {
    const machines = buildAgentRuntimeMachines([
      rt({ id: "a", daemon_id: null, owner_id: "u1", name: "Claude (box)" }),
      rt({ id: "b", daemon_id: null, owner_id: "u2", name: "Claude (box)" }),
    ]);
    expect(machines).toHaveLength(2);
  });

  it("treats a cloud runtime as its own machine", () => {
    const machines = buildAgentRuntimeMachines([
      rt({
        id: "c",
        daemon_id: null,
        runtime_mode: "cloud",
        name: "Codex cloud",
        device_info: "Cloud · us-west",
        owner_id: null,
      }),
    ]);
    expect(machines).toHaveLength(1);
    expect(machines[0]!.cloud).toBe(true);
    expect(machines[0]!.label).toBe("Cloud · us-west");
    expect(machines[0]!.ownerId).toBeNull();
  });

  it("marks a machine online when any of its runtimes is online", () => {
    const machines = buildAgentRuntimeMachines([
      rt({ id: "a", daemon_id: "d1", provider: "claude", status: "offline" }),
      rt({ id: "b", daemon_id: "d1", provider: "codex", status: "online" }),
    ]);
    expect(machines[0]!.online).toBe(true);
  });

  it("falls back to the runtime id when daemon and device are both missing", () => {
    const machines = buildAgentRuntimeMachines([
      rt({ id: "x", daemon_id: null, name: "Claude", device_info: "" }),
    ]);
    expect(machines).toHaveLength(1);
    expect(machines[0]!.label).toBe("Claude");
  });
});

describe("isRuntimeUsableForUser", () => {
  it("allows the owner, allows public, blocks another member's private", () => {
    expect(isRuntimeUsableForUser(rt({ owner_id: "u1" }), "u1")).toBe(true);
    expect(
      isRuntimeUsableForUser(rt({ owner_id: "u2", visibility: "public" }), "u1"),
    ).toBe(true);
    expect(
      isRuntimeUsableForUser(rt({ owner_id: "u2", visibility: "private" }), "u1"),
    ).toBe(false);
  });
});
