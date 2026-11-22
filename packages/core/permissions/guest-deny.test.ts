// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, Comment, MemberRole, RuntimeDevice, Skill } from "../types";
import { canDeleteDoc } from "../docs/deletion-permissions";
import { canManageIssueView } from "../issue-views/queries";
import { canDeleteIssue } from "../issues/deletion-permissions";
import { canChangeProjectLead, canManageProjectAccess } from "../projects/access";
import { canDeleteProject } from "../projects/deletion-permissions";
import * as rules from "./rules";

// Guests (MUL-9420) are deny-by-default: every role-gated helper must refuse a
// guest, including on what the guest created or owned before it was made a
// guest. Each case pairs the guest with the role that IS allowed on the same
// fixture, so a case can't pass by never reaching the creator/owner branch.
// A new role-gated helper belongs in this table.

const ME = "user-me";
const OTHER = "user-other";

const myAgent = { id: "agt_mine", owner_id: ME } as Agent;
const agents = [myAgent];
const ownedAgent = {
  ...myAgent,
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
} as Agent;
const mySkill = { id: "skl_1", created_by: ME } as Skill;
const myRuntime = { id: "rt_1", owner_id: ME } as RuntimeDevice;
const ledProject = { lead_type: "member", lead_id: ME, visibility: "private" } as const;
const openProject = { lead_type: null, lead_id: null, visibility: "workspace" } as const;

type Case = [name: string, allowedAs: MemberRole, check: (role: MemberRole) => boolean];

const cases: Case[] = [
  ["canDeleteIssue (own issue)", "member", (role) => canDeleteIssue({ creator_type: "member", creator_id: ME }, ME, role, agents)],
  ["canDeleteIssue (own agent's issue)", "member", (role) => canDeleteIssue({ creator_type: "agent", creator_id: myAgent.id }, ME, role, agents)],
  ["canDeleteDoc (own doc)", "member", (role) => canDeleteDoc({ createdByType: "member", createdById: ME }, ME, role, agents)],
  ["canDeleteDoc (own agent's doc)", "member", (role) => canDeleteDoc({ createdByType: "agent", createdById: myAgent.id }, ME, role, agents)],
  ["canDeleteProject (own project)", "member", (role) => canDeleteProject({ creator_type: "member", creator_id: ME }, ME, role, agents)],
  ["canManageProjectAccess (lead of a private project)", "member", (role) => canManageProjectAccess(ledProject, ME, role, ME)],
  ["canChangeProjectLead (workspace-visible project)", "member", (role) => canChangeProjectLead(openProject, ME, role, null)],
  ["canChangeProjectLead (lead of a private project)", "member", (role) => canChangeProjectLead(ledProject, ME, role, ME)],
  ["canManageIssueView (own view)", "member", (role) => canManageIssueView({ owner_id: ME, visibility: "workspace" }, ME, role)],
  ["canEditAgent (own agent)", "member", (role) => rules.canEditAgent(myAgent, { userId: ME, role }).allowed],
  ["canAssignAgentToIssue (own agent)", "member", (role) => rules.canAssignAgentToIssue(ownedAgent, { userId: ME, role }).allowed],
  ["canAssignAgentToIssue (workspace-shared agent)", "member", (role) => rules.canAssignAgentToIssue(ownedAgent, { userId: OTHER, role }).allowed],
  ["canEditSkill (own skill)", "member", (role) => rules.canEditSkill(mySkill, { userId: ME, role }).allowed],
  ["canDeleteSkill (own skill)", "member", (role) => rules.canDeleteSkill(mySkill, { userId: ME, role }).allowed],
  ["canDeleteRuntime (own runtime)", "member", (role) => rules.canDeleteRuntime(myRuntime, { userId: ME, role }).allowed],
  ["canUpdateWorkspaceSettings", "admin", (role) => rules.canUpdateWorkspaceSettings({ userId: ME, role }).allowed],
  ["canDeleteWorkspace", "owner", (role) => rules.canDeleteWorkspace({ userId: ME, role }).allowed],
  ["canManageMembers", "admin", (role) => rules.canManageMembers({ userId: ME, role }).allowed],
  ["canChangeMemberRole", "admin", (role) => rules.canChangeMemberRole({ role: "member" }, 1, { userId: ME, role }).allowed],
];

// Routes the server's guest allowlist lets through, so a guest may pass.
const GUEST_ALLOWED_RULES = new Set(["canEditComment", "canDeleteComment"]);

describe("guest deny-by-default", () => {
  it.each(cases)("%s denies a guest", (_name, allowedAs, check) => {
    expect(check(allowedAs)).toBe(true);
    expect(check("guest")).toBe(false);
  });

  it("covers every rule in rules.ts", () => {
    const covered = new Set(cases.map(([name]) => name.split(" ")[0]));
    const missing = Object.keys(rules).filter(
      (name) => name.startsWith("can") && !covered.has(name) && !GUEST_ALLOWED_RULES.has(name),
    );
    expect(missing).toEqual([]);
  });

  it("lets a guest edit and delete only its own comments", () => {
    const mine = { author_type: "member", author_id: ME } as Comment;
    const theirs = { author_type: "member", author_id: OTHER } as Comment;
    const guest = { userId: ME, role: "guest" } as const;
    expect(rules.canEditComment(mine, guest).allowed).toBe(true);
    expect(rules.canDeleteComment(mine, guest).allowed).toBe(true);
    expect(rules.canEditComment(theirs, guest).allowed).toBe(false);
    expect(rules.canDeleteComment(theirs, guest).allowed).toBe(false);
  });
});
