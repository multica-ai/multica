// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { MemberWithUser } from "@multica/core/types";
import { isGuestRole, memberRoleOf } from "./member-role";

function member(userId: string, role: MemberWithUser["role"]): MemberWithUser {
  return {
    id: `m-${userId}`,
    workspace_id: "ws-1",
    user_id: userId,
    role,
    created_at: "2026-01-01T00:00:00Z",
    name: userId,
    email: `${userId}@example.com`,
    avatar_url: null,
  };
}

describe("memberRoleOf", () => {
  const members = [member("owner-1", "owner"), member("guest-1", "guest")];

  it("returns the current user's role", () => {
    expect(memberRoleOf(members, "guest-1")).toBe("guest");
    expect(memberRoleOf(members, "owner-1")).toBe("owner");
  });

  it("is null while the list loads, signed out, or for a non-member", () => {
    expect(memberRoleOf(undefined, "guest-1")).toBeNull();
    expect(memberRoleOf(members, null)).toBeNull();
    expect(memberRoleOf(members, "stranger")).toBeNull();
  });
});

describe("isGuestRole", () => {
  it("is true only for a guest (MUL-9420)", () => {
    expect(isGuestRole("guest")).toBe(true);
    for (const role of ["owner", "admin", "member", null, undefined] as const) {
      expect(isGuestRole(role)).toBe(false);
    }
  });
});
