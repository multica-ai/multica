// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canDeleteIssue } from "./deletion-permissions";
import type { MemberRole } from "../types";

const agents = [{ id: "owned", owner_id: "me" }, { id: "other", owner_id: "someone" }];
describe("issue deletion permissions", () => {
  it.each([
    [false, "member", "member", "someone", true],
    [true, "member", "member", "me", true],
    [true, "member", "member", "someone", false],
    [true, "member", "agent", "owned", true],
    [true, "member", "agent", "other", false],
    [true, "member", "agent", "deleted", false],
    [true, "owner", "member", "someone", true],
    [true, "admin", "agent", "other", true],
    [true, null, "member", "me", false],
    [true, "member", "unknown", "me", false],
  ] as const)("restricted=%s role=%s creator=%s:%s allows=%s", (restricted, role, type, id, expected) => {
    expect(canDeleteIssue({ creator_type: type as "member" | "agent", creator_id: id }, restricted, "me", role as MemberRole | null, agents)).toBe(expected);
  });
  it("does not grant former owners access after an agent changes owner", () => {
    expect(canDeleteIssue({creator_type: "agent", creator_id: "owned"}, true, "me", "member", [{id: "owned", owner_id: "someone"}])).toBe(false);
  });
});
