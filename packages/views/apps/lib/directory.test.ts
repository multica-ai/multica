// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AppCatalogEntry, AppConnection, AppsOverview } from "@multica/core/types";
import { buildAppDirectory, matchesAppSearch } from "./directory";
import { agentAppsWith } from "./agent-apps";

function connection(slug: string, status: AppConnection["status"] = "active"): AppConnection {
  return { id: `${slug}-${status}`, toolkit_slug: slug, status, connected_at: "", last_used_at: null };
}

function overview(patch: Partial<AppsOverview> = {}): AppsOverview {
  return { catalog_mode: "all", can_manage: false, team: [], personal: [], agent_apps: [], ...patch };
}

const catalog: AppCatalogEntry[] = [
  { slug: "gmail", name: "Gmail", category: "Email", curated: true },
  { slug: "notion", name: "Notion", category: "Docs", curated: true },
  { slug: "slack", name: "Slack", category: "Chat", curated: true },
  { slug: "zendesk", name: "Zendesk", category: "Support", curated: false },
];

const build = (o: AppsOverview, extra: { viewerId?: string } = {}) =>
  buildAppDirectory({ overview: o, catalog, viewerId: extra.viewerId ?? "me" });

const slugs = (apps: { slug: string }[]) => apps.map((a) => a.slug);

describe("buildAppDirectory", () => {
  it("counts an app in use when it has an account or an agent uses it", () => {
    const dir = build(overview({
      team: [connection("notion")],
      personal: [connection("slack")],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "write" }],
    }));
    expect(slugs(dir.inUse)).toEqual(["gmail", "notion", "slack"]);
    expect(slugs(dir.all)).toEqual(["gmail", "notion", "slack", "zendesk"]);
    expect(slugs(dir.popular)).toEqual([]);
  });

  it("flags an app agents use through an account that is missing or expired", () => {
    const dir = build(overview({
      team: [connection("notion", "expired")],
      agent_apps: [
        { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "write" },
        { agent_id: "a1", toolkit_slug: "slack", account_scope: "team", level: "write" },
        { agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "write" },
      ],
    }));
    const byslug = Object.fromEntries(dir.all.map((a) => [a.slug, a]));
    expect(slugs(dir.attention)).toEqual(["gmail", "notion", "slack"]);
    expect(byslug.gmail).toMatchObject({
      status: "not_connected",
      action: { kind: "connect", scope: "personal", reconnect: false },
    });
    expect(byslug.slack).toMatchObject({ status: "not_connected", action: { kind: "ask_admin" } });
    // Members cannot reconnect a team account; they ask an admin to.
    expect(byslug.notion).toMatchObject({ status: "needs_reconnecting", action: { kind: "ask_admin" } });
  });

  it("lets an admin connect or reconnect the team account", () => {
    const dir = build(overview({
      can_manage: true,
      team: [connection("notion", "expired")],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "slack", account_scope: "team", level: "write" }],
    }));
    const byslug = Object.fromEntries(dir.all.map((a) => [a.slug, a]));
    expect(byslug.slack!.action).toEqual({ kind: "connect", scope: "team", reconnect: false });
    expect(byslug.notion!.action).toEqual({ kind: "connect", scope: "team", reconnect: true });
  });

  it("shows the most pressing status across both accounts", () => {
    const dir = build(overview({
      team: [connection("gmail")],
      personal: [connection("gmail", "expired")],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "gmail", account_scope: "team", level: "write" }],
    }));
    const gmail = dir.all.find((a) => a.slug === "gmail")!;
    expect(gmail.scopes).toEqual(["team", "personal"]);
    expect(gmail).toMatchObject({
      status: "needs_reconnecting",
      statusScope: "personal",
      needsAttention: true,
      action: { kind: "connect", scope: "personal", reconnect: true },
    });
  });

  it("lists apps that need attention first among those in use", () => {
    const dir = build(overview({
      team: [connection("gmail"), connection("notion")],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "slack", account_scope: "team", level: "write" }],
    }));
    expect(slugs(dir.inUse)).toEqual(["slack", "gmail", "notion"]);
  });

  it("does not flag connected accounts", () => {
    const dir = build(overview({
      team: [connection("notion")],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "write" }],
    }));
    expect(dir.attention).toEqual([]);
    expect(dir.inUse[0]).toMatchObject({ slug: "notion", status: "connected", action: null, agentIds: ["a1"] });
  });

  it("puts open team requests in front of admins only, and marks the viewer's own", () => {
    const requests = [{ toolkit_slug: "slack", requested_by: ["me", "u2"], asked_by: ["me", "u2"], agent_ids: [], last_requested_at: "" }];
    const member = build(overview({ team_requests: requests }));
    const slack = member.all.find((a) => a.slug === "slack")!;
    expect(slack).toMatchObject({ inUse: false, needsAttention: false, scopes: ["team"], action: { kind: "withdraw" } });
    expect(slugs(member.popular)).not.toContain("slack");

    const other = build(overview({ team_requests: requests }), { viewerId: "u3" });
    expect(other.all.find((a) => a.slug === "slack")!.action).toEqual({ kind: "ask_admin" });

    const admin = build(overview({ can_manage: true, team_requests: requests }));
    expect(slugs(admin.attention)).toEqual(["slack"]);
    expect(admin.attention[0]!.action).toEqual({ kind: "connect", scope: "team", reconnect: false });
  });

  it("ignores requests once the team account exists", () => {
    const dir = build(overview({
      can_manage: true,
      team: [connection("slack")],
      team_requests: [{ toolkit_slug: "slack", requested_by: ["u2"], agent_ids: [], last_requested_at: "" }],
    }));
    expect(dir.attention).toEqual([]);
    expect(dir.all.find((a) => a.slug === "slack")!.request).toBeUndefined();
  });

  it("leaves out revoked accounts", () => {
    const dir = build(overview({ personal: [connection("notion", "revoked")] }));
    expect(dir.inUse).toEqual([]);
    expect(dir.attention).toEqual([]);
  });

  it("offers curated apps nobody uses as popular, and set-up for unused ones", () => {
    const dir = build(overview({ team: [connection("notion")] }));
    expect(slugs(dir.popular)).toEqual(["gmail", "slack"]);
    expect(dir.popular[0]!.action).toEqual({ kind: "set_up" });
  });

  it("lists accounts for apps the catalog no longer shows after the catalog", () => {
    const dir = build(overview({ team: [connection("legacy-crm")] }));
    expect(slugs(dir.all)).toEqual(["gmail", "notion", "slack", "zendesk", "legacy-crm"]);
    expect(dir.all.at(-1)!.name).toBe("legacy-crm");
  });
});

describe("matchesAppSearch", () => {
  const [gmail] = build(overview()).all;
  it.each([["", true], ["gm", true], ["GMAIL", true], ["email", true], ["notion", false]])(
    "matches %j: %s",
    (query, expected) => {
      expect(matchesAppSearch(gmail!, query)).toBe(expected);
    },
  );
});

describe("agentAppsWith", () => {
  const base = overview({
    agent_apps: [
      { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "read" },
      { agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "write" },
      { agent_id: "a2", toolkit_slug: "slack", account_scope: "team", level: "write" },
    ],
  });

  it("keeps the agent's other apps and levels when one changes", () => {
    expect(agentAppsWith(base, "a1", "gmail", "team")).toEqual({
      apps: [
        { toolkit_slug: "notion", account_scope: "team", level: "write" },
        { toolkit_slug: "gmail", account_scope: "team", level: "read" },
      ],
    });
  });

  it("adds a new app with write access and removes one with null", () => {
    expect(agentAppsWith(base, "a2", "gmail", "personal").apps).toEqual([
      { toolkit_slug: "slack", account_scope: "team", level: "write" },
      { toolkit_slug: "gmail", account_scope: "personal", level: "write" },
    ]);
    expect(agentAppsWith(base, "a1", "notion", null).apps).toEqual([
      { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
    ]);
  });
});
