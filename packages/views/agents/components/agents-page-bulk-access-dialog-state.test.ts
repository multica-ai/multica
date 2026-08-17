import { describe, it, expect } from "vitest";
import { isAccessChangeReady } from "@multica/core/agents";
import type { AccessChange } from "./inspector/access-picker";

function change(
  permission_mode: AccessChange["permission_mode"],
  memberIds: string[] = [],
): AccessChange {
  return {
    permission_mode,
    invocation_targets: memberIds.map((id) => ({
      target_type: "member",
      target_id: id,
    })),
  };
}

const workspaceTarget: AccessChange = {
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: "" }],
};

describe("isAccessChangeReady — bulk-access dialog confirm gate", () => {
  it("null change is not ready (no preselect)", () => {
    expect(isAccessChangeReady(null)).toBe(false);
  });

  it("Owner-only is ready with zero targets (the only valid clear state)", () => {
    expect(isAccessChangeReady(change("private"))).toBe(true);
  });

  it("public_to with a workspace target is ready", () => {
    expect(isAccessChangeReady(workspaceTarget)).toBe(true);
  });

  it("public_to with ≥1 member target is ready", () => {
    expect(isAccessChangeReady(change("public_to", ["user-1"]))).toBe(true);
  });

  it("public_to with zero invocation_targets is NOT ready", () => {
    // The AccessPicker requires ≥1 workspace or member target before
    // emitting a public_to change, so this only happens if the caller
    // bypasses the picker. isAccessChangeReady stays safe.
    expect(isAccessChangeReady(change("public_to", []))).toBe(false);
  });
});
