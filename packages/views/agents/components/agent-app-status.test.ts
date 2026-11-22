// @vitest-environment node

import { describe, expect, it } from "vitest";
import type { AppConnection, AppsOverview } from "@multica/core/types";
import { needsViewerAccount, scopeForNewApp } from "./agent-app-status";

function account(status: string): AppConnection {
  return { id: `c-${status}`, toolkit_slug: "slack", status, connected_at: "", last_used_at: null };
}

function overview({
  admin,
  team,
  personal,
}: {
  admin: boolean;
  team?: string;
  personal?: string;
}): AppsOverview {
  return {
    catalog_mode: "all",
    can_manage: admin,
    team: team ? [account(team)] : [],
    personal: personal ? [account(personal)] : [],
    agent_apps: [],
  };
}

describe("scopeForNewApp", () => {
  it.each([
    { name: "admin, team account, usually team", admin: true, team: "active", personal: "active", recommended: "team", scope: "team" },
    { name: "admin, team account, no account of their own", admin: true, team: "active", personal: undefined, recommended: undefined, scope: "team" },
    { name: "admin, team account, own account, not usually team", admin: true, team: "active", personal: "active", recommended: "personal", scope: "personal" },
    { name: "admin, team account, own account expired", admin: true, team: "active", personal: "expired", recommended: undefined, scope: "team" },
    { name: "admin, expired team account", admin: true, team: "expired", personal: undefined, recommended: "team", scope: "personal" },
    { name: "admin, no team account", admin: true, team: undefined, personal: undefined, recommended: "team", scope: "personal" },
    { name: "member, team account, usually team", admin: false, team: "active", personal: undefined, recommended: "team", scope: "personal" },
  ] as const)("$name → $scope", ({ admin, team, personal, recommended, scope }) => {
    expect(scopeForNewApp(overview({ admin, team, personal }), "slack", recommended, true)).toBe(scope);
  });

  it("falls back to personal before the overview loads", () => {
    expect(scopeForNewApp(undefined, "slack", "team", true)).toBe("personal");
  });

  it("keeps new apps off the team account while the plan lacks team accounts", () => {
    expect(scopeForNewApp(overview({ admin: true, team: "active" }), "slack", "team", false)).toBe("personal");
  });
});

describe("needsViewerAccount", () => {
  it("flags personal-account apps the viewer has not connected or must reconnect", () => {
    expect(needsViewerAccount("personal", "not_connected")).toBe(true);
    expect(needsViewerAccount("personal", "needs_reconnecting")).toBe(true);
    expect(needsViewerAccount("personal", "connected")).toBe(false);
    expect(needsViewerAccount("team", "not_connected")).toBe(false);
  });
});
