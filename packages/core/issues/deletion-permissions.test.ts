// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canDeleteIssue } from "./deletion-permissions";
import type { MemberRole } from "../types";

const agents = [{ id: "owned", owner_id: "me" }, { id: "other", owner_id: "someone" }];
describe("issue deletion permissions", () => {
  it.each([
    ["member", "member", "me", true],
    ["member", "member", "someone", false],
    ["member", "agent", "owned", true],
    ["member", "agent", "other", false],
    ["member", "agent", "deleted", false],
    ["owner", "member", "someone", true],
    ["admin", "agent", "other", true],
    ["admin", "agent", "deleted", true],
    [null, "member", "me", false],
    ["member", "unknown", "me", false],
  ] as const)("role=%s creator=%s:%s allows=%s", (role, type, id, expected) => {
    expect(canDeleteIssue({ creator_type: type as "member" | "agent", creator_id: id }, "me", role as MemberRole | null, agents)).toBe(expected);
  });
  it("does not grant former owners access after an agent changes owner", () => {
    expect(canDeleteIssue({ creator_type: "agent", creator_id: "owned" }, "me", "member", [{ id: "owned", owner_id: "someone" }])).toBe(false);
  });
});
