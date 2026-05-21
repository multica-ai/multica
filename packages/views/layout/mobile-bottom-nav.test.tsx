import { describe, it, expect } from "vitest";
import { isTabActive } from "./mobile-bottom-nav";

describe("isTabActive", () => {
  it("matches the exact tab href", () => {
    expect(isTabActive("/acme/issues", "/acme/issues")).toBe(true);
  });

  it("stays active on a descendant route (issue detail)", () => {
    expect(isTabActive("/acme/issues/MUL-123", "/acme/issues")).toBe(true);
  });

  it("does not match a sibling that shares a prefix", () => {
    // /acme/issues-archive must not light up the /acme/issues tab.
    expect(isTabActive("/acme/issues-archive", "/acme/issues")).toBe(false);
  });

  it("does not match an unrelated route", () => {
    expect(isTabActive("/acme/inbox", "/acme/issues")).toBe(false);
  });

  it("does not match across workspaces", () => {
    expect(isTabActive("/foo/issues", "/acme/issues")).toBe(false);
  });
});
