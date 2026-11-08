// @vitest-environment node
import { describe, expect, it } from "vitest";
import { WorkspaceLinkSchema, WorkspaceLinksSchema, EMPTY_WORKSPACE_LINK } from "./schemas";
import { parseWithFallback } from "./schema";
import type { WorkspaceLink } from "../types/workspace-link";

const link = {
  id: "link",
  workspace_id: "workspace",
  name: "Docs",
  url_template: "https://example.com",
};

describe("workspace link response compatibility", () => {
  it("maps only consumed fields and defaults sandbox relaxation off", () => {
    expect(WorkspaceLinkSchema.parse({ ...link, future: true })).toEqual({
      id: "link",
      workspaceId: "workspace",
      projectId: null,
      name: "Docs",
      urlTemplate: "https://example.com",
      icon: "",
      showName: true,
      openMode: "new_tab",
      allowSameOrigin: false,
      position: 0,
    });
  });
  it("uses an external-opening fallback for unknown modes", () => {
    expect(WorkspaceLinkSchema.parse({ ...link, open_mode: "future" }).openMode).toBe("new_tab");
  });
  it("defaults a missing show_name to true and preserves explicit false", () => {
    expect(WorkspaceLinkSchema.parse(link).showName).toBe(true);
    expect(WorkspaceLinkSchema.parse({ ...link, show_name: false }).showName).toBe(false);
  });
  it.each([
    { ...link, name: 1 },
    { ...link, url_template: null },
    { ...link, allow_same_origin: "true" },
    { ...link, show_name: "false" },
    {},
  ])("rejects malformed used fields %j", (raw) => {
    expect(
      parseWithFallback<WorkspaceLink>(raw, WorkspaceLinkSchema, EMPTY_WORKSPACE_LINK, { endpoint: "test" }),
    ).toBe(EMPTY_WORKSPACE_LINK);
  });
  it("rejects malformed catalogs", () => {
    expect(
      parseWithFallback<{ links: WorkspaceLink[] }>(
        { links: [{}] },
        WorkspaceLinksSchema,
        { links: [] },
        { endpoint: "test" },
      ),
    ).toEqual({ links: [] });
  });
});
