import { describe, expect, it } from "vitest";
import type { Agent, Comment, Member, RuntimeDevice, Skill } from "../types";
import {
  canAssignAgentToIssue,
  canChangeMemberRole,
  canDeleteComment,
  canDeleteRuntime,
  canDeleteSkill,
  canDeleteWorkspace,
  canEditAgent,
  canEditComment,
  canEditSkill,
  canManageMembers,
  canUpdateWorkspaceSettings,
} from "./rules";

const ALICE = "user-alice";
const BOB = "user-bob";

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
    owner_id: ALICE,
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeSkill(createdBy: string | null): Skill {
  return {
    id: "skl_1",
    workspace_id: "ws_1",
    name: "skill",
    description: "",
    content: "",
    config: {},
    files: [],
    created_by: createdBy,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
}

function makeComment(overrides: Partial<Comment> = {}): Comment {
  return {
    id: "cmt_1",
    issue_id: "iss_1",
    author_type: "member",
    author_id: ALICE,
    content: "hi",
    type: "comment",
    parent_id: null,
    reactions: [],
    attachments: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    resolved_at: null,
    resolved_by_type: null,
    resolved_by_id: null,
    ...overrides,
  };
}

function makeRuntime(ownerId: string | null): RuntimeDevice {
  return {
    id: "rt_1",
    workspace_id: "ws_1",
    daemon_id: null,
    name: "runtime",
    runtime_mode: "local",
    provider: "anthropic",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: ownerId,
    visibility: "private",
    timezone: "UTC",
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
}

describe("canEditAgent", () => {
  const agent = makeAgent({ owner_id: ALICE });

  it("allows the owner", () => {
    expect(canEditAgent(agent, { userId: ALICE, role: "member" }).allowed).toBe(
      true,
    );
  });
  it("allows workspace owner", () => {
    expect(canEditAgent(agent, { userId: BOB, role: "owner" }).allowed).toBe(
      true,
    );
  });
  it("allows workspace admin", () => {
    expect(canEditAgent(agent, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
  it("denies non-owner member", () => {
    const d = canEditAgent(agent, { userId: BOB, role: "member" });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_resource_owner");
  });
  it("denies when userId is null", () => {
    const d = canEditAgent(agent, { userId: null, role: null });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_authenticated");
  });
  it("denies when agent owner_id is null and user is plain member", () => {
    const orphan = makeAgent({ owner_id: null });
    expect(
      canEditAgent(orphan, { userId: ALICE, role: "member" }).allowed,
    ).toBe(false);
  });
  it("admin can still edit an orphan (owner_id null) agent", () => {
    const orphan = makeAgent({ owner_id: null });
    expect(canEditAgent(orphan, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
});

describe("canAssignAgentToIssue", () => {
  it("allows any member to assign workspace-visibility agents", () => {
    const a = makeAgent({ visibility: "workspace", owner_id: ALICE });
    expect(
      canAssignAgentToIssue(a, { userId: BOB, role: "member" }).allowed,
    ).toBe(true);
  });
  it("denies non-members from assigning workspace agents", () => {
    const a = makeAgent({ visibility: "workspace", owner_id: ALICE });
    const d = canAssignAgentToIssue(a, { userId: BOB, role: null });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_member");
  });
  it("allows the owner to assign their private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: ALICE });
    expect(
      canAssignAgentToIssue(a, { userId: ALICE, role: "member" }).allowed,
    ).toBe(true);
  });
  it("allows workspace admin to assign someone else's private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: ALICE });
    expect(
      canAssignAgentToIssue(a, { userId: BOB, role: "admin" }).allowed,
    ).toBe(true);
  });
  it("denies a plain member from assigning someone else's private agent", () => {
    const a = makeAgent({ visibility: "private", owner_id: ALICE });
    const d = canAssignAgentToIssue(a, { userId: BOB, role: "member" });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("private_visibility");
  });
  it("denies logged-out users", () => {
    const a = makeAgent({ visibility: "workspace" });
    const d = canAssignAgentToIssue(a, { userId: null, role: null });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_authenticated");
  });
});

describe("canEditSkill / canDeleteSkill", () => {
  const skill = makeSkill(ALICE);
  it("allows admins", () => {
    expect(canEditSkill(skill, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
  it("allows the creator", () => {
    expect(canEditSkill(skill, { userId: ALICE, role: "member" }).allowed)
      .toBe(true);
  });
  it("denies non-creator member", () => {
    expect(canEditSkill(skill, { userId: BOB, role: "member" }).allowed)
      .toBe(false);
  });
  it("denies when created_by is null and user is plain member", () => {
    expect(
      canEditSkill(makeSkill(null), { userId: ALICE, role: "member" }).allowed,
    ).toBe(false);
  });
  it("canDeleteSkill mirrors canEditSkill", () => {
    expect(canDeleteSkill(skill, { userId: ALICE, role: "member" }).allowed)
      .toBe(true);
    expect(canDeleteSkill(skill, { userId: BOB, role: "member" }).allowed)
      .toBe(false);
  });
});

describe("canEditComment / canDeleteComment", () => {
  it("allows the author to edit their own comment", () => {
    const c = makeComment({ author_id: ALICE });
    expect(canEditComment(c, { userId: ALICE, role: "member" }).allowed).toBe(
      true,
    );
  });
  it("allows workspace admin to edit someone else's comment", () => {
    const c = makeComment({ author_id: ALICE });
    expect(canEditComment(c, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
  it("denies non-author non-admin", () => {
    const c = makeComment({ author_id: ALICE });
    expect(canEditComment(c, { userId: BOB, role: "member" }).allowed).toBe(
      false,
    );
  });
  it("denies edit on agent-authored comments", () => {
    const c = makeComment({ author_type: "agent", author_id: "agt_1" });
    const d = canEditComment(c, { userId: BOB, role: "owner" });
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_resource_owner");
  });
  it("admin CAN delete an agent-authored comment", () => {
    // delete is broader than edit — admins moderate any comment regardless of
    // author type. Mirrors backend `comment.go:507-512`.
    const c = makeComment({ author_type: "agent", author_id: "agt_1" });
    expect(canDeleteComment(c, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
  it("denies plain member from deleting agent-authored comment", () => {
    const c = makeComment({ author_type: "agent", author_id: "agt_1" });
    expect(
      canDeleteComment(c, { userId: BOB, role: "member" }).allowed,
    ).toBe(false);
  });
});

describe("canDeleteRuntime", () => {
  it("allows the owner", () => {
    const r = makeRuntime(ALICE);
    expect(canDeleteRuntime(r, { userId: ALICE, role: "member" }).allowed)
      .toBe(true);
  });
  it("allows workspace admin", () => {
    const r = makeRuntime(ALICE);
    expect(canDeleteRuntime(r, { userId: BOB, role: "admin" }).allowed).toBe(
      true,
    );
  });
  it("denies non-owner non-admin", () => {
    const r = makeRuntime(ALICE);
    expect(canDeleteRuntime(r, { userId: BOB, role: "member" }).allowed)
      .toBe(false);
  });
});

describe("workspace-level rules", () => {
  it("only owner can delete workspace", () => {
    expect(canDeleteWorkspace({ userId: ALICE, role: "owner" }).allowed).toBe(
      true,
    );
    expect(canDeleteWorkspace({ userId: ALICE, role: "admin" }).allowed).toBe(
      false,
    );
    expect(canDeleteWorkspace({ userId: ALICE, role: "member" }).allowed)
      .toBe(false);
  });
  it("owner+admin can update settings, member cannot", () => {
    expect(
      canUpdateWorkspaceSettings({ userId: ALICE, role: "owner" }).allowed,
    ).toBe(true);
    expect(
      canUpdateWorkspaceSettings({ userId: ALICE, role: "admin" }).allowed,
    ).toBe(true);
    expect(
      canUpdateWorkspaceSettings({ userId: ALICE, role: "member" }).allowed,
    ).toBe(false);
  });
  it("manage members same gate as settings", () => {
    expect(canManageMembers({ userId: ALICE, role: "admin" }).allowed).toBe(
      true,
    );
    expect(canManageMembers({ userId: ALICE, role: "member" }).allowed).toBe(
      false,
    );
  });
});

describe("canChangeMemberRole", () => {
  const ctxOwner = { userId: ALICE, role: "owner" as const };
  const ctxAdmin = { userId: ALICE, role: "admin" as const };
  const ctxMember = { userId: ALICE, role: "member" as const };

  const targetOwner: Pick<Member, "role"> = { role: "owner" };
  const targetAdmin: Pick<Member, "role"> = { role: "admin" };
  const targetMember: Pick<Member, "role"> = { role: "member" };

  it("non-managers cannot change roles", () => {
    expect(canChangeMemberRole(targetMember, 2, ctxMember).allowed).toBe(false);
  });
  it("admin cannot change owner's role", () => {
    const d = canChangeMemberRole(targetOwner, 2, ctxAdmin);
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("not_owner_role");
  });
  it("admin can change admin/member roles", () => {
    expect(canChangeMemberRole(targetAdmin, 1, ctxAdmin).allowed).toBe(true);
    expect(canChangeMemberRole(targetMember, 1, ctxAdmin).allowed).toBe(true);
  });
  it("owner cannot demote the last owner", () => {
    const d = canChangeMemberRole(targetOwner, 1, ctxOwner);
    expect(d.allowed).toBe(false);
    expect(d.reason).toBe("last_owner");
  });
  it("owner can change owner role when 2+ owners exist", () => {
    expect(canChangeMemberRole(targetOwner, 2, ctxOwner).allowed).toBe(true);
  });
});
