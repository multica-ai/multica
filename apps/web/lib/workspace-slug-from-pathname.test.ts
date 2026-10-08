// @vitest-environment node
import { describe, it, expect } from "vitest";
import { workspaceSlugFromPathname } from "./workspace-slug-from-pathname";

describe("workspaceSlugFromPathname", () => {
  it("reads the first segment of a workspace route", () => {
    expect(workspaceSlugFromPathname("/acme/issues")).toBe("acme");
    expect(workspaceSlugFromPathname("/acme")).toBe("acme");
    expect(workspaceSlugFromPathname("/acme/issues/ACME-1")).toBe("acme");
  });

  it("tolerates trailing and doubled separators", () => {
    expect(workspaceSlugFromPathname("/acme/")).toBe("acme");
    expect(workspaceSlugFromPathname("//acme//issues")).toBe("acme");
  });

  it("returns null when there is no first segment", () => {
    expect(workspaceSlugFromPathname("/")).toBeNull();
    expect(workspaceSlugFromPathname("")).toBeNull();
    expect(workspaceSlugFromPathname(null)).toBeNull();
    expect(workspaceSlugFromPathname(undefined)).toBeNull();
  });

  it("decodes the segment so it can compare equal to a route param", () => {
    // Route params arrive decoded; the pathname segment does not.
    expect(workspaceSlugFromPathname("/a%20b/issues")).toBe("a b");
    expect(workspaceSlugFromPathname("/caf%C3%A9")).toBe("café");
  });

  it("falls back to the raw segment when it is not valid encoding", () => {
    expect(workspaceSlugFromPathname("/100%/issues")).toBe("100%");
  });

  it("returns the first segment of a global route, which no slug equals", () => {
    // Reserved slugs are rejected at creation, so a workspace layout
    // comparing its own slug against these can never match.
    expect(workspaceSlugFromPathname("/login")).toBe("login");
    expect(workspaceSlugFromPathname("/workspaces/new")).toBe("workspaces");
  });
});
