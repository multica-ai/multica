import { describe, expect, it } from "vitest";
import type { Workspace } from "../types";
import { workspaceBySlugOptions } from "./queries";

function makeWorkspace(slug: string): Workspace {
  return {
    id: `id-${slug}`,
    name: slug,
    slug,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: slug.toUpperCase(),
    avatar_url: null,
    created_at: "",
    updated_at: "",
  };
}

describe("workspaceBySlugOptions", () => {
  const workspaces = [makeWorkspace("acme")];

  it("selects a matching workspace", () => {
    expect(workspaceBySlugOptions("acme").select?.(workspaces)).toEqual(
      workspaces[0],
    );
  });

  it("returns null after an authoritative list omits the slug", () => {
    expect(workspaceBySlugOptions("missing").select?.(workspaces)).toBeNull();
  });
});
