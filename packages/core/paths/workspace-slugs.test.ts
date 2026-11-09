// @vitest-environment node
import { afterEach, describe, expect, it } from "vitest";
import { isReservedSlug } from "./reserved-slugs";
import {
  isKnownWorkspacePath,
  setWorkspaceSlugLookup,
  workspaceSlugForPath,
} from "./workspace-slugs";

afterEach(() => setWorkspaceSlugLookup(() => false));

describe("workspaceSlugForPath", () => {
  it.each(["links", "dingtalk"])("resolves start for reserved workspace %s with membership", (slug) => {
    expect(workspaceSlugForPath(`/${slug}/start`)).toBeNull();
    setWorkspaceSlugLookup((candidate) => candidate === slug);
    expect(workspaceSlugForPath(`/${slug}/start`)).toBe(slug);
    expect(isKnownWorkspacePath(`/${slug}/start`)).toBe(true);
  });

  it("recognizes embedded link paths for a workspace member", () => {
    setWorkspaceSlugLookup((slug) => slug === "acme");
    expect(workspaceSlugForPath("/acme/links/id")).toBe("acme");
    expect(isKnownWorkspacePath("/acme/links/id")).toBe(true);
  });

  it("resolves link paths for an existing reserved-name workspace", () => {
    setWorkspaceSlugLookup((slug) => slug === "links");
    expect(workspaceSlugForPath("/links/links/id")).toBe("links");
    expect(workspaceSlugForPath("/links/links/id", "links")).toBe("links");
    expect(isKnownWorkspacePath("/links/links/id")).toBe(true);
    expect(isReservedSlug("links")).toBe(true);
  });

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
