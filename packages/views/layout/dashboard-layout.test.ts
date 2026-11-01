// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isOfficePath } from "./dashboard-layout";

describe("isOfficePath", () => {
  it("is the office of any workspace", () => {
    expect(isOfficePath("/acme/office")).toBe(true);
    expect(isOfficePath("/acme/office/")).toBe(true);
  });

  it("is no other page", () => {
    expect(isOfficePath("/acme/issues")).toBe(false);
    expect(isOfficePath("/acme/offices")).toBe(false);
    expect(isOfficePath("/office")).toBe(false);
    expect(isOfficePath("/acme/projects/office")).toBe(false);
  });
});
