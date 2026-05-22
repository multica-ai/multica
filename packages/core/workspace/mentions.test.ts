import { describe, expect, it } from "vitest";
import type { Agent, Issue, MemberWithUser } from "../types";
import {
  buildWorkspaceMentionTargets,
  issueToMentionTarget,
  sortMentionTargetsByFrequency,
} from "./mentions";

function member(overrides: Partial<MemberWithUser>): MemberWithUser {
  return {
    id: overrides.id ?? "member-row",
    workspace_id: overrides.workspace_id ?? "ws-1",
    user_id: overrides.user_id ?? "user-1",
    role: overrides.role ?? "member",
    created_at: overrides.created_at ?? "2026-01-01T00:00:00Z",
    name: overrides.name ?? "Alice",
    email: overrides.email ?? "alice@example.com",
    avatar_url: overrides.avatar_url ?? null,
  };
}

function agent(overrides: Partial<Agent>): Agent {
  return {
    id: overrides.id ?? "agent-1",
    workspace_id: overrides.workspace_id ?? "ws-1",
    runtime_id: overrides.runtime_id ?? "runtime-1",
    name: overrides.name ?? "Builder",
    description: overrides.description ?? "",
    instructions: overrides.instructions ?? "",
    avatar_url: overrides.avatar_url ?? null,
    runtime_mode: overrides.runtime_mode ?? "cloud",
    runtime_config: overrides.runtime_config ?? {},
    custom_env: overrides.custom_env ?? {},
    custom_args: overrides.custom_args ?? [],
    custom_env_redacted: overrides.custom_env_redacted ?? false,
    visibility: overrides.visibility ?? "workspace",
    status: overrides.status ?? "idle",
    max_concurrent_tasks: overrides.max_concurrent_tasks ?? 1,
    model: overrides.model ?? "test-model",
    owner_id: overrides.owner_id ?? null,
    allowed_user_ids: overrides.allowed_user_ids,
    skills: overrides.skills ?? [],
    created_at: overrides.created_at ?? "2026-01-01T00:00:00Z",
    updated_at: overrides.updated_at ?? "2026-01-01T00:00:00Z",
    archived_at: overrides.archived_at ?? null,
  } as Agent;
}

describe("workspace mention targets", () => {
  it("filters private agents from other users but keeps workspace agents", () => {
    const targets = buildWorkspaceMentionTargets(
      [member({ user_id: "user-1", name: "Alice" })],
      [
        agent({ id: "workspace-agent", name: "Team Bot", visibility: "workspace" }),
        agent({ id: "other-ws-agent", name: "Other WS Bot", visibility: "workspace", owner_id: "other-user" }),
        agent({ id: "private-agent", name: "Private Bot", visibility: "private", owner_id: "other-user" }),
        agent({ id: "own-agent", name: "My Bot", visibility: "private", owner_id: "user-1" }),
      ],
      { userId: "user-1", role: "member" },
    );

    expect(targets.map((target) => `${target.type}:${target.label}`)).toEqual([
      "all:All members",
      "member:Alice",
      "agent:Team Bot",
      "agent:Other WS Bot",
      "agent:My Bot",
    ]);
  });

  it("hides private agents from other users even for workspace admin", () => {
    const targets = buildWorkspaceMentionTargets(
      [member({ user_id: "admin-1", name: "Admin", role: "admin" })],
      [
        agent({ id: "other-ws-agent", name: "Other WS Bot", visibility: "workspace", owner_id: "other-user" }),
        agent({ id: "private-agent", name: "Private Bot", visibility: "private", owner_id: "other-user" }),
        agent({ id: "own-agent", name: "My Bot", owner_id: "admin-1" }),
        agent({ id: "legacy-agent", name: "Legacy Bot", owner_id: null }),
      ],
      { userId: "admin-1", role: "admin" },
    );

    const agentLabels = targets.filter((t) => t.type === "agent").map((t) => t.label);
    // Workspace agents are shared — always visible.
    expect(agentLabels).toContain("Other WS Bot");
    // Private agents from other users are hidden even for admins.
    expect(agentLabels).not.toContain("Private Bot");
    expect(agentLabels).toContain("My Bot");
    expect(agentLabels).toContain("Legacy Bot");
  });

  it("keeps allowlisted private agents visible to the current user", () => {
    const targets = buildWorkspaceMentionTargets(
      [member({ user_id: "user-1", name: "Alice" })],
      [
        agent({
          id: "allowed-private-agent",
          name: "Allowed Bot",
          visibility: "private",
          owner_id: "other-user",
          allowed_user_ids: ["user-1"],
        }),
      ],
      { userId: "user-1", role: "member" },
    );

    const agentLabels = targets.filter((t) => t.type === "agent").map((t) => t.label);
    expect(agentLabels).toContain("Allowed Bot");
  });

  it("maps issues to issue mention targets", () => {
    const issue = {
      id: "issue-1",
      identifier: "MUL-123",
      title: "Ship mobile mentions",
      status: "in_progress",
    } as Issue;

    expect(issueToMentionTarget(issue)).toEqual({
      id: "issue-1",
      label: "MUL-123",
      type: "issue",
      description: "Ship mobile mentions",
      status: "in_progress",
    });
  });

  it("sorts mention targets by mention frequency", () => {
    const sorted = sortMentionTargetsByFrequency(
      [
        { id: "alice", label: "Alice", type: "member" },
        { id: "bot", label: "Bot", type: "agent" },
        { id: "zoe", label: "Zoe", type: "member" },
      ],
      [
        {
          actor_type: "agent",
          actor_id: "bot",
          frequency: 2,
          last_mentioned_at: "2026-01-01T00:00:00Z",
        },
        {
          actor_type: "member",
          actor_id: "zoe",
          frequency: 4,
          last_mentioned_at: "2026-01-01T00:00:00Z",
        },
      ],
    );

    expect(sorted.map((target) => target.label)).toEqual(["Zoe", "Bot", "Alice"]);
  });

  it("prioritizes the current user's own agents and demotes All members", () => {
    const sorted = sortMentionTargetsByFrequency(
      [
        { id: "all", label: "All members", type: "all" },
        { id: "alice", label: "Alice", type: "member" },
        { id: "shared-bot", label: "Aardvark Shared", type: "agent" },
        { id: "my-bot", label: "My Bot", type: "agent" },
      ],
      [
        {
          actor_type: "member",
          actor_id: "alice",
          frequency: 9,
          last_mentioned_at: "2026-01-01T00:00:00Z",
        },
      ],
      { ownAgentIds: ["my-bot"] },
    );

    expect(sorted.map((target) => `${target.type}:${target.id}`)).toEqual([
      "agent:my-bot",
      "member:alice",
      "agent:shared-bot",
      "all:all",
    ]);
  });
});
