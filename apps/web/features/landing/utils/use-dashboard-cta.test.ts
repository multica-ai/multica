import { describe, expect, it } from "vitest";
import { paths } from "@multica/core/paths";
import type { Workspace } from "@multica/core/types";
import { resolveDashboardCtaHref } from "./use-dashboard-cta";

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

const fetched = (workspaces: Workspace[], hasOnboarded = true) => ({
  isAuthenticated: true,
  isWorkspaceListFetched: true,
  workspaces,
  hasOnboarded,
});

describe("resolveDashboardCtaHref", () => {
  it("sends logged-out visitors to /login", () => {
    expect(
      resolveDashboardCtaHref({
        isAuthenticated: false,
        isWorkspaceListFetched: false,
        workspaces: undefined,
        hasOnboarded: false,
      }),
    ).toBe(paths.login());
  });

  // The bug this hook exists to fix: the CTA used to be `/`, which on the
  // public marketing host resolves to the page the visitor is already on, so
  // the click did nothing. It must resolve to a real workspace route.
  it("sends an onboarded visitor to their workspace, never back to the landing page", () => {
    const href = resolveDashboardCtaHref(fetched([makeWs("acme")]));
    expect(href).toBe(paths.workspace("acme").issues());
    expect(href).not.toBe("/");
  });

  it("sends an un-onboarded visitor to /onboarding", () => {
    expect(resolveDashboardCtaHref(fetched([makeWs("acme")], false))).toBe(
      paths.onboarding(),
    );
  });

  it("sends an onboarded visitor with no workspace to /workspaces/new", () => {
    expect(resolveDashboardCtaHref(fetched([]))).toBe(paths.newWorkspace());
  });

  it.each([
    ["the list has not resolved yet", false, undefined],
    ["the list resolved as undefined", true, undefined],
  ])("falls back to /issues while %s", (_label, isWorkspaceListFetched, workspaces) => {
    // /issues is a legacy route the proxy rewrites to the last workspace, so
    // the button still works during hydration. It must not fall back to `/`,
    // which is what made the CTA dead in the first place.
    const href = resolveDashboardCtaHref({
      isAuthenticated: true,
      isWorkspaceListFetched,
      workspaces,
      hasOnboarded: true,
    });
    expect(href).toBe("/issues");
    expect(href).not.toBe("/");
  });
});
