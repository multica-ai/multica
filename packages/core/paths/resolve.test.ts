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
    channels_enabled: false,
    channel_retention_days: null,
  };
}

describe("resolvePostAuthDestination", () => {
  it("has workspace → /<first.slug>/issues regardless of onboarded state", () => {
    const ws = [makeWs("acme"), makeWs("beta")];
    expect(resolvePostAuthDestination(ws, true)).toBe(
      paths.workspace("acme").issues(),
    );
    expect(resolvePostAuthDestination(ws, false)).toBe(
      paths.workspace("acme").issues(),
    );
    expect(resolvePostAuthDestination([makeWs("acme")], false)).toBe(
      paths.workspace("acme").issues(),
    );
  });

  it("zero workspaces + !onboarded → /onboarding", () => {
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
  });

  it("zero workspaces + onboarded → /workspaces/new", () => {
    expect(resolvePostAuthDestination([], true)).toBe(paths.newWorkspace());
  });
});
