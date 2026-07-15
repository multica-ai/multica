import { describe, expect, it } from "vitest";
import type { Agent, Space } from "../types";
import { chatSpacesForAgent, defaultChatSpaceId } from "./context";

function makeSpace(id: string, overrides: Partial<Space> = {}): Space {
  return {
    id,
    workspace_id: "ws-1",
    name: id,
    key: id.toUpperCase(),
    icon: null,
    context: "",
    issue_counter: 0,
    is_default: false,
    visibility: "open",
    archived_at: null,
    created_by: "user-1",
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    is_member: true,
    member_role: "member",
    is_pinned: false,
    is_followed: false,
    sort_order: 0,
    ...overrides,
  };
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    owner_id: "user-1",
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
    invocation_targets: [{ target_type: "workspace", target_id: null }],
    availability_mode: "workspace",
    availability_space_ids: [],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "sonnet",
    skills: [],
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("chatSpacesForAgent", () => {
  const spaces = [
    makeSpace("alpha"),
    makeSpace("guest", { member_role: "guest" }),
    makeSpace("unjoined", { is_member: false, member_role: null }),
    makeSpace("archived", { archived_at: new Date(0).toISOString() }),
  ];

  it("returns the member and Agent availability intersection", () => {
    const agent = makeAgent({
      availability_mode: "selected_spaces",
      availability_space_ids: ["alpha", "guest", "unjoined"],
    });

    expect(chatSpacesForAgent(agent, spaces, "user-1", "member").map((s) => s.id)).toEqual([
      "alpha",
    ]);
  });

  it("lets workspace admins collaborate in every active Space", () => {
    expect(chatSpacesForAgent(makeAgent(), spaces, "admin-1", "admin").map((s) => s.id)).toEqual([
      "alpha",
      "guest",
      "unjoined",
    ]);
  });

  it("keeps private Agents owner-only", () => {
    const agent = makeAgent({ availability_mode: "private" });
    expect(chatSpacesForAgent(agent, spaces, "user-2", "member")).toEqual([]);
    expect(chatSpacesForAgent(agent, spaces, "user-1", "member").map((s) => s.id)).toEqual([
      "alpha",
    ]);
  });
});

describe("defaultChatSpaceId", () => {
  it("defaults one eligible Space by name and multiple Spaces to All", () => {
    const alpha = makeSpace("alpha");
    expect(defaultChatSpaceId([alpha])).toBe("alpha");
    expect(defaultChatSpaceId([alpha, makeSpace("beta")])).toBeNull();
  });
});
