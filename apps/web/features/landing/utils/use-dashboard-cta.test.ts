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

const fetched = (workspaces: Workspace[]) => ({
  isAuthenticated: true,
  workspaceListReady: true,
  workspaces,
});

describe("resolveDashboardCtaHref", () => {
  it("sends logged-out visitors to /login", () => {
    expect(
      resolveDashboardCtaHref({
        isAuthenticated: false,
        workspaceListReady: false,
        workspaces: [],
      }),
    ).toBe(paths.login());
  });

  // The bug this hook exists to fix: the CTA used to be `/`, which on the
  // public marketing host resolves to the page the visitor is already on, so
  // the click did nothing. It must resolve to a real workspace route.
  it("sends a visitor to their workspace, never back to the landing page", () => {
    const href = resolveDashboardCtaHref(fetched([makeWs("acme")]));
    expect(href).toBe("/tag/acme/chat");
    expect(href).not.toBe("/");
  });

  it("sends a visitor with no workspace to the Tag authority create route", () => {
    expect(resolveDashboardCtaHref(fetched([]))).toBe("/tag/workspaces/new");
  });

  it("falls back to the Tag authority while the list has not resolved yet", () => {
    const href = resolveDashboardCtaHref({
      isAuthenticated: true,
      workspaceListReady: false,
      workspaces: [],
    });
    expect(href).toBe("/tag/workspaces/new");
    expect(href).not.toBe("/");
  });
});
