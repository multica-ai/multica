import { describe, expect, it } from "vitest";
import { withRoleDecision, type RolePermissions } from "./roles";

describe("withRoleDecision", () => {
  it("stores a capability-wide decision in the canonical rule-list shape", () => {
    expect(withRoleDecision({}, "issue.create", "ask")).toEqual({
      "issue.create": [{ setting: "ask", resource_pattern: "", conditions: null }],
    });
  });

  it("preserves narrower resource rules when the capability-wide decision changes", () => {
    const existing: RolePermissions = {
      "repo.write": [
        { setting: "deny", resource_pattern: "", conditions: null },
        { setting: "allow", resource_pattern: "github.com/firtal-group/docs", conditions: null },
      ],
    };

    expect(withRoleDecision(existing, "repo.write", "ask")).toEqual({
      "repo.write": [
        { setting: "ask", resource_pattern: "", conditions: null },
        { setting: "allow", resource_pattern: "github.com/firtal-group/docs", conditions: null },
      ],
    });
  });

  it("removes only the capability-wide rule when a Role inherits", () => {
    const existing: RolePermissions = {
      "repo.write": [
        { setting: "deny", resource_pattern: "", conditions: null },
        { setting: "allow", resource_pattern: "github.com/firtal-group/docs", conditions: null },
      ],
    };

    expect(withRoleDecision(existing, "repo.write", "inherit")).toEqual({
      "repo.write": [
        { setting: "allow", resource_pattern: "github.com/firtal-group/docs", conditions: null },
      ],
    });
  });
});
