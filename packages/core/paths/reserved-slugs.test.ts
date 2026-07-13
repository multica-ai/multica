import { describe, expect, it } from "vitest";
import { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";

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

  it("exposes a non-empty reserved slug set", () => {
    expect(RESERVED_SLUGS.size).toBeGreaterThan(0);
  });

  it("keeps the set and predicate consistent", () => {
    for (const slug of RESERVED_SLUGS) {
      expect(isReservedSlug(slug)).toBe(true);
    }
  });

  it("matches slugs case-sensitively", () => {
    expect(isReservedSlug("Login")).toBe(false);
  });
});
