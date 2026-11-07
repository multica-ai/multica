// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { isReservedSlug } from "./reserved-slugs";
import { setWorkspaceSlugLookup, workspaceSlugForPath } from "./workspace-slugs";

afterEach(() => setWorkspaceSlugLookup(() => false));

describe("workspaceSlugForPath", () => {
  it("admits existing reserved workspaces without changing creation policy", () => {
    expect(workspaceSlugForPath("/dingtalk/issues")).toBeNull();
    setWorkspaceSlugLookup((slug) => slug === "dingtalk");
    expect(workspaceSlugForPath("/dingtalk/issues?filter=mine#comment")).toBe("dingtalk");
    expect(workspaceSlugForPath("/dingtalk/members/member-1")).toBe("dingtalk");
    expect(workspaceSlugForPath("/dingtalk/attachments/file-1/preview")).toBe("dingtalk");
    expect(isReservedSlug("dingtalk")).toBe(true);
  });

  it.each(["/dingtalk/bind", "/auth/callback", "/workspaces/new", "/invite/token", "/api/workspaces", "/uploads/file", "/login", "/"])("keeps %s global even with a matching membership", (path) => {
    setWorkspaceSlugLookup(() => true);
    expect(workspaceSlugForPath(path)).toBeNull();
  });

  it("only preserves matching saved workspace routes", () => {
    expect(workspaceSlugForPath("/dingtalk/issues", "dingtalk")).toBe("dingtalk");
    expect(workspaceSlugForPath("/dingtalk/issues", "acme")).toBeNull();
    expect(workspaceSlugForPath("/dingtalk/bind", "dingtalk")).toBeNull();
    expect(workspaceSlugForPath("/acme/issues")).toBe("acme");
    expect(workspaceSlugForPath("/%broken/issues")).toBeNull();
  });
});
