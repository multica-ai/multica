// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canDeleteDoc } from "./deletion-permissions";

const agents = [
  { id: "owned", owner_id: "me" },
  { id: "other", owner_id: "someone" },
];
describe("doc deletion permissions", () => {
  it.each([
    ["member", "member", "me", true],
    ["member", "member", "someone", false],
    ["member", "agent", "owned", true],
    ["member", "agent", "other", false],
    ["member", "agent", "deleted", false],
    ["owner", "member", "someone", true],
    ["admin", "agent", "other", true],
    [null, "member", "me", false],
  ] as const)("role=%s creator=%s:%s allows=%s", (role, type, id, expected) => {
    expect(
      canDeleteDoc({ createdByType: type, createdById: id }, "me", role, agents),
    ).toBe(expected);
  });
  it("requires a signed-in user even for admins", () => {
    expect(
      canDeleteDoc({ createdByType: "member", createdById: "me" }, null, "admin", agents),
    ).toBe(false);
  });
  it("does not grant former owners access after an agent changes owner", () => {
    expect(
      canDeleteDoc({ createdByType: "agent", createdById: "owned" }, "me", "member", [
        { id: "owned", owner_id: "someone" },
      ]),
    ).toBe(false);
  });
});
