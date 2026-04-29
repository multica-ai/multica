import { describe, expect, it } from "vitest";
import type { Workspace } from "../types";
import { paths } from "./paths";
import { resolvePostAuthDestination } from "./resolve";

function makeWs(slug: string): Workspace {
  return {
    id: `id-${slug}`,
    name: slug,
    slug,
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: slug.toUpperCase(),
    created_at: "",
    updated_at: "",
  };
}

describe("resolvePostAuthDestination", () => {
  it("has workspace → /<first.slug>/issues regardless of onboarding", () => {
    expect(resolvePostAuthDestination([makeWs("acme")], false)).toBe(
      paths.workspace("acme").issues(),
    );
    expect(resolvePostAuthDestination([makeWs("acme")], true)).toBe(
      paths.workspace("acme").issues(),
    );
    expect(
      resolvePostAuthDestination([makeWs("acme"), makeWs("beta")], false),
    ).toBe(paths.workspace("acme").issues());
  });

  it("not onboarded + zero workspaces → /onboarding", () => {
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
  });

  it("onboarded + zero workspaces → /workspaces/new", () => {
    expect(resolvePostAuthDestination([], true)).toBe(paths.newWorkspace());
  });
});
