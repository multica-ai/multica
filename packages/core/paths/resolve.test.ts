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
  it("!onboarded → /onboarding regardless of workspace count", () => {
    // Un-onboarded users are routed back to the onboarding flow. The
    // "un-onboarded but in workspace" state is now physically impossible
    // (backend invariant + migration 065 backfill), but the resolver still
    // does the right thing if it ever appears: send the user to onboarding
    // rather than dropping them into a workspace with `onboarded_at` null.
    expect(resolvePostAuthDestination([], false)).toBe(paths.onboarding());
    expect(resolvePostAuthDestination([makeWs("acme")], false)).toBe(
      paths.onboarding(),
    );
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
