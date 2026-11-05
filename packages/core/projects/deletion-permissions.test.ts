// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canDeleteProject } from "./deletion-permissions";
import type { Project } from "../types";

const agents = [{ id: "owned", owner_id: "me" }, { id: "other", owner_id: "someone" }];
describe("project deletion permissions", () => {
  it.each([
    ["member", "member", "me", true],
    ["member", "member", "someone", false],
    ["member", "agent", "owned", true],
    ["member", "agent", "other", false],
    ["member", "agent", "deleted", false],
    ["owner", "member", "someone", true],
    ["admin", "agent", "other", true],
    [null, "member", "me", false],
    ["member", "unknown", "me", false],
    ["member", null, null, false],
    ["member", undefined, undefined, false],
    ["member", "member", null, false],
    ["member", null, "me", false],
    ["admin", null, null, true],
    ["owner", undefined, undefined, true],
  ] as const)("role=%s creator=%s:%s allows=%s", (role, type, id, expected) => {
    expect(canDeleteProject({ creator_type: type as Project["creator_type"], creator_id: id }, "me", role, agents)).toBe(expected);
  });
  it("requires a signed-in user even for admins", () => {
    expect(canDeleteProject({}, null, "admin", agents)).toBe(false);
  });
  it("does not grant former owners access after an agent changes owner", () => {
    expect(canDeleteProject({ creator_type: "agent", creator_id: "owned" }, "me", "member", [{ id: "owned", owner_id: "someone" }])).toBe(false);
  });
});
