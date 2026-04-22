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
  it("not onboarded → /onboarding regardless of workspaces", () => {
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
    expect(resolvePostAuthDestination([makeWs("acme")], false)).toBe(
      paths.onboarding(),
    );
    expect(
      resolvePostAuthDestination([makeWs("acme"), makeWs("beta")], false),
    ).toBe(paths.onboarding());
  });

  it("onboarded + has workspace → /<first.slug>/issues", () => {
    const ws = [makeWs("acme"), makeWs("beta")];
    expect(resolvePostAuthDestination(ws, true)).toBe(
      paths.workspace("acme").issues(),
    );
  });

  it("onboarded + zero workspaces → /workspaces/new", () => {
    expect(resolvePostAuthDestination([], true)).toBe(paths.newWorkspace());
  });
});
