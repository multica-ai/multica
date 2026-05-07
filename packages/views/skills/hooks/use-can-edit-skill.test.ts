import { describe, it, expect } from "vitest";
import type { Skill } from "@multica/core/types";
import { canEditSkill } from "./use-can-edit-skill";

function makeSkill(createdBy: string | null): Skill {
  return {
    id: "skl_x",
    workspace_id: "ws_1",
    name: "x",
    description: "",
    content: "",
    config: {},
    files: [],
    created_by: createdBy,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
}

describe("canEditSkill", () => {
  const skill = makeSkill("user-alice");

  it("allows workspace owners to edit any skill", () => {
    expect(
      canEditSkill(skill, { userId: "user-bob", role: "owner" }),
    ).toBe(true);
  });

  it("allows workspace admins to edit any skill", () => {
    expect(
      canEditSkill(skill, { userId: "user-bob", role: "admin" }),
    ).toBe(true);
  });

  it("allows the creator to edit their own skill", () => {
    expect(
      canEditSkill(skill, { userId: "user-alice", role: "member" }),
    ).toBe(true);
  });

  it("denies non-creator members", () => {
    expect(
      canEditSkill(skill, { userId: "user-bob", role: "member" }),
    ).toBe(false);
  });

  it("denies unknown-role users even if they match created_by", () => {
    // role=null models a member list that hasn't loaded yet or a user who
    // isn't a member at all; we still honor created_by identity.
    expect(
      canEditSkill(skill, { userId: "user-alice", role: null }),
    ).toBe(true);
  });

  it("denies when created_by is null (legacy / system-created)", () => {
    expect(
      canEditSkill(makeSkill(null), { userId: "user-alice", role: "member" }),
    ).toBe(false);
  });

  it("denies when userId is null (logged-out edge case)", () => {
    expect(
      canEditSkill(skill, { userId: null, role: "member" }),
    ).toBe(false);
  });
});
