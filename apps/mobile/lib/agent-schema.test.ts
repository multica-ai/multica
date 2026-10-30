import { describe, expect, it } from "vitest";
import { AgentSchema } from "../data/schemas";

describe("AgentSchema invocation permissions", () => {
  it("defaults missing invocation permissions to private access", () => {
    const parsed = AgentSchema.parse({ id: "agent-1" });

    expect(parsed.permission_mode).toBe("private");
    expect(parsed.invocation_targets).toEqual([]);
  });

  it("parses public invocation grants", () => {
    const parsed = AgentSchema.parse({
      id: "agent-1",
      permission_mode: "public_to",
      invocation_targets: [
        { target_type: "workspace" },
        { target_type: "member", target_id: "member-1" },
      ],
    });

    expect(parsed.permission_mode).toBe("public_to");
    expect(parsed.invocation_targets).toEqual([
      { target_type: "workspace", target_id: null },
      { target_type: "member", target_id: "member-1" },
    ]);
  });

  it("fails closed for unknown permission values", () => {
    const parsed = AgentSchema.parse({
      id: "agent-1",
      permission_mode: "future_mode",
      invocation_targets: [{ target_type: "future_target", target_id: 123 }],
    });

    expect(parsed.permission_mode).toBe("private");
    expect(parsed.invocation_targets).toEqual([
      { target_type: "team", target_id: null },
    ]);
  });

  it("preserves the additive runtime binding signal", () => {
    const parsed = AgentSchema.parse({
      id: "agent-1",
      runtime_id: "",
      runtime_bound: false,
    });

    expect(parsed.runtime_id).toBe("");
    expect(parsed.runtime_bound).toBe(false);
  });

  it("parses conversation starters and degrades malformed additive data", () => {
    expect(
      AgentSchema.parse({
        id: "agent-1",
        conversation_starters: [
          { label: "Review a PR", prompt: "Review the open pull request." },
        ],
      }).conversation_starters,
    ).toEqual([
      { label: "Review a PR", prompt: "Review the open pull request." },
    ]);

    expect(
      AgentSchema.parse({ id: "agent-1", conversation_starters: "invalid" })
        .conversation_starters,
    ).toEqual([]);
  });
});
