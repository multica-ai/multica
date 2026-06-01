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
  it("!onboarded + workspace[0] → /<first.slug>/issues (skip onboarding)", () => {
    // Onboarding guide is skipped — new users with a workspace land
    // directly on their first workspace.
    const ws = [makeWs("acme")];
    expect(resolvePostAuthDestination(ws, false)).toBe(
      paths.workspace("acme").issues(),
    );
  });

  it("!onboarded + no workspace → /workspaces/new (skip onboarding)", () => {
    // Onboarding guide is skipped — new users without a workspace go
    // straight to workspace creation.
    expect(resolvePostAuthDestination([], false)).toBe(paths.newWorkspace());
  });

  it("onboarded + workspace[0] → /<first.slug>/issues", () => {
    const ws = [makeWs("acme"), makeWs("beta")];
    expect(resolvePostAuthDestination(ws, true)).toBe(
      paths.workspace("acme").issues(),
    );
  });

  it("onboarded + no workspace → /workspaces/new", () => {
    // Already-onboarded user without any workspace — usually a returning
    // user whose last workspace got deleted or who left it. They skip
    // re-onboarding and go straight to workspace creation.
    expect(resolvePostAuthDestination([], true)).toBe(paths.newWorkspace());
  });
});
