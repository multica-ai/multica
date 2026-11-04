// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canManageProjects } from "./permissions";

describe("canManageProjects", () => {
  it.each([
    ["owner", true],
    ["admin", true],
    ["member", false],
    [undefined, false],
    [null, false],
    ["", false],
    ["unknown", false],
  ])("role %s -> %s", (role, expected) => {
    expect(canManageProjects(role)).toBe(expected);
  });
});
