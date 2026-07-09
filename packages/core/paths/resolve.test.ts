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
    avatar_url: null,
    created_at: "",
    updated_at: "",
  };
}

describe("resolvePostAuthDestination", () => {
  it("!onboarded → /onboarding (even with a workspace)", () => {
    // V3 invariant: onboarded_at is the single source of truth for
    // workspace access. A user holding workspaces but flagged !onboarded
    // (rare mid-flow state: closed app between Step 2 and Step 3) gets
    // routed to /onboarding so they can finish; the layout hard gate
    // would redirect them anyway.
    const ws = [makeWs("acme")];
    expect(resolvePostAuthDestination(ws, false)).toBe(paths.onboarding());
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
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
