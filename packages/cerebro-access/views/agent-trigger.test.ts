import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import { canTriggerPrivateAgentMention } from "./agent-trigger";

const OWNER = "user-owner";
const OTHER = "user-other";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agt_1",
    workspace_id: "ws_1",
    runtime_id: "rt_1",
    name: "agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "default",
    owner_id: OWNER,
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    persona_sandbox: "",
    ...overrides,
  };
}

describe("canTriggerPrivateAgentMention", () => {
  it("allows any member to trigger a workspace-visibility agent", () => {
    const a = makeAgent({ visibility: "workspace" });
    expect(
      canTriggerPrivateAgentMention(a, { userId: OTHER, role: "member" })
        .allowed,
    ).toBe(true);
  });

  it("allows the owner to trigger their private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: OWNER });
    expect(
      canTriggerPrivateAgentMention(a, { userId: OWNER, role: "member" })
        .allowed,
    ).toBe(true);
  });

  it("blocks a non-owner member from triggering a private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: OWNER });
    const d = canTriggerPrivateAgentMention(a, {
      userId: OTHER,
      role: "member",
    });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("private_visibility");
  });

  it("blocks an admin (no override) from triggering someone else's private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: OWNER });
    expect(
      canTriggerPrivateAgentMention(a, { userId: OTHER, role: "admin" })
        .allowed,
    ).toBe(false);
  });

  it("denies when not authenticated", () => {
    const a = makeAgent({ visibility: "private", owner_id: OWNER });
    const d = canTriggerPrivateAgentMention(a, { userId: null, role: null });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_authenticated");
  });
});
