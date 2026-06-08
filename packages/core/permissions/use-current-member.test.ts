import { describe, expect, it } from "vitest";
import type { MemberRole, MemberWithUser } from "../types";
import { resolveCurrentMember } from "./use-current-member";

function member(id: string, userId: string, role: MemberRole): MemberWithUser {
  return {
    id,
    workspace_id: "ws_1",
    user_id: userId,
    role,
    created_at: "2026-06-08T00:00:00Z",
    name: "Test User",
    email: "test@example.com",
    avatar_url: null,
    budget_enforcement_enabled: false,
  };
}

describe("resolveCurrentMember", () => {
  it("returns null without members or a user id", () => {
    expect(resolveCurrentMember(undefined, "user_1")).toBeNull();
    expect(resolveCurrentMember([member("m1", "user_1", "owner")], null)).toBeNull();
  });

  it("returns the matching member when the user has one membership", () => {
    const current = member("m1", "user_1", "admin");
    expect(resolveCurrentMember([member("m2", "user_2", "owner"), current], "user_1")).toBe(
      current,
    );
  });

  it("uses the strongest role when duplicate memberships exist for the user", () => {
    const oldMember = member("m1", "user_1", "member");
    const owner = member("m2", "user_1", "owner");
    const admin = member("m3", "user_1", "admin");

    expect(resolveCurrentMember([oldMember, admin, owner], "user_1")).toBe(owner);
    expect(resolveCurrentMember([oldMember, admin], "user_1")).toBe(admin);
  });
});
