import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import { canAssignAgent } from "./can-assign-agent";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "workspace-1",
    runtime_id: "runtime-1",
    name: "Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [
      { target_type: "workspace", target_id: null },
    ],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "owner-1",
    skills: [],
    created_at: "",
    updated_at: "",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("canAssignAgent availability", () => {
  const selectedAgent = makeAgent({
    availability_mode: "selected_spaces",
    availability_space_ids: ["space-1"],
  });

  it("allows a Selected-Spaces Agent in a matching Issue Space", () => {
    expect(
      canAssignAgent(selectedAgent, "member-1", "member", "space-1"),
    ).toBe(true);
  });

  it("excludes a Selected-Spaces Agent from a different Issue Space", () => {
    expect(
      canAssignAgent(selectedAgent, "member-1", "member", "space-2"),
    ).toBe(false);
  });

  it("excludes a Selected-Spaces Agent from context-free Chat", () => {
    expect(canAssignAgent(selectedAgent, "member-1", "member", null)).toBe(
      false,
    );
  });

  it("keeps Workspace Agents available in context-free Chat", () => {
    expect(
      canAssignAgent(
        makeAgent({ availability_mode: "workspace" }),
        "member-1",
        "member",
        null,
      ),
    ).toBe(true);
  });
});
