import { describe, expect, it } from "vitest";
import { resolveAutopilotControlPermissions } from "./autopilot-control-permissions";

const row = (tool_key: string, setting: "allow" | "ask" | "deny") => ({
  tool_key,
  resource_pattern: "",
  effective: { setting, decided_by: "user", capped_by: "", reason: "", openable: true },
});

describe("resolveAutopilotControlPermissions", () => {
  it("uses the existing effective decisions and fails Ask/Deny closed", () => {
    expect(resolveAutopilotControlPermissions("member", [row("create_autopilot", "allow"), row("trigger_autopilot", "deny")])).toEqual({ canManage: true, canTrigger: false });
    expect(resolveAutopilotControlPermissions("member", [row("create_autopilot", "ask"), row("trigger_autopilot", "allow")])).toEqual({ canManage: false, canTrigger: true });
  });

  it("preserves the owner/admin override", () => {
    expect(resolveAutopilotControlPermissions("admin", [])).toEqual({ canManage: true, canTrigger: true });
  });
});
