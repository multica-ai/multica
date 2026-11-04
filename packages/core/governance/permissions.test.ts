// @vitest-environment node
import { describe, expect, it } from "vitest";
import { canGovern } from "./permissions";

describe("canGovern", () => {
  it("lets owners and admins govern", () => {
    expect(canGovern("owner")).toBe(true);
    expect(canGovern("admin")).toBe(true);
  });

  it("refuses members and an unknown role", () => {
    expect(canGovern("member")).toBe(false);
    expect(canGovern(null)).toBe(false);
    expect(canGovern(undefined)).toBe(false);
  });
});
