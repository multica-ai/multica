import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import {
  extractMentionedAgentIds,
  pickAgentNeedingConfirm,
} from "./private-agent-confirm";

const ME = "user-me";
const OWNER = "user-owner";

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

const foreignPrivate = makeAgent({
  id: "agt_foreign",
  name: "Maria's agent",
  visibility: "private",
  owner_id: OWNER,
});
const myPrivate = makeAgent({
  id: "agt_mine",
  visibility: "private",
  owner_id: ME,
});
const workspaceAgent = makeAgent({
  id: "agt_ws",
  visibility: "workspace",
  owner_id: null,
});

describe("extractMentionedAgentIds", () => {
  it("pulls agent ids from mention markdown, de-duplicated", () => {
    const md =
      "hi [@A](mention://agent/agt_a) and [@B](mention://agent/agt_b) and [@A](mention://agent/agt_a)";
    expect(extractMentionedAgentIds(md)).toEqual(["agt_a", "agt_b"]);
  });

  it("ignores member mentions", () => {
    expect(
      extractMentionedAgentIds("[@Bob](mention://member/user-bob)"),
    ).toEqual([]);
  });
});

describe("pickAgentNeedingConfirm", () => {
  const agents = [foreignPrivate, myPrivate, workspaceAgent];
  const base = { agents, userId: ME, role: "member" as const };

  it("confirms when the trigger agent is a foreign private agent", () => {
    const picked = pickAgentNeedingConfirm({
      ...base,
      markdown: "please look at this",
      triggerAgentId: foreignPrivate.id,
    });
    expect(picked?.id).toBe(foreignPrivate.id);
  });

  it("confirms when a foreign private agent is explicitly @mentioned", () => {
    const picked = pickAgentNeedingConfirm({
      ...base,
      markdown: `hey [@Maria's agent](mention://agent/${foreignPrivate.id})`,
      triggerAgentId: undefined,
    });
    expect(picked?.id).toBe(foreignPrivate.id);
  });

  it("does NOT confirm for my own private agent", () => {
    expect(
      pickAgentNeedingConfirm({
        ...base,
        markdown: "ping",
        triggerAgentId: myPrivate.id,
      }),
    ).toBeNull();
  });

  it("does NOT confirm for a workspace agent", () => {
    expect(
      pickAgentNeedingConfirm({
        ...base,
        markdown: "ping",
        triggerAgentId: workspaceAgent.id,
      }),
    ).toBeNull();
  });

  it("does NOT confirm when no agent is targeted", () => {
    expect(
      pickAgentNeedingConfirm({
        ...base,
        markdown: "just a note",
        triggerAgentId: undefined,
      }),
    ).toBeNull();
  });

  it("explicit @mention overrides the trigger-agent fallback", () => {
    // Drafting a tag of a workspace agent while the issue's trigger agent is
    // foreign-private → the explicit tag wins, so no confirm.
    const picked = pickAgentNeedingConfirm({
      ...base,
      markdown: `[@ws](mention://agent/${workspaceAgent.id})`,
      triggerAgentId: foreignPrivate.id,
    });
    expect(picked).toBeNull();
  });
});
