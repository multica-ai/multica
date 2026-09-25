import { describe, expect, it } from "vitest";
import { isReservedSlug } from "./reserved-slugs";

describe("reserved slugs", () => {
  it("returns true for a known reserved slug", () => {
    expect(isReservedSlug("login")).toBe(true);
  });

  it("returns false for an unreserved slug", () => {
    expect(isReservedSlug("my-cool-workspace")).toBe(false);
  });

  it("returns false for an empty slug", () => {
    expect(isReservedSlug("")).toBe(false);
  });

  it("matches slugs case-sensitively", () => {
    expect(isReservedSlug("Login")).toBe(false);
  });
});
