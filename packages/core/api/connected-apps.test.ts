import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { EMPTY_APPS_OVERVIEW } from "./schemas";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respondWith(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return { client: new ApiClient("https://api.example.test"), fetchMock };
}

const request = {
  id: "r1",
  kind: "action",
  status: "pending",
  agent_id: "a1",
  issue_id: "i1",
  approver_user_id: "u1",
  account_scope: "personal",
  toolkit_slug: "gmail",
  tool_slug: "GMAIL_SEND_EMAIL",
  action_class: "send",
  summary: "Send the report",
  arguments: { to: "x@example.com" },
  error: null,
  reason: null,
  redacted: false,
  expires_at: "2026-11-05T00:00:00Z",
  decided_by: null,
  decided_at: null,
  created_at: "2026-11-04T00:00:00Z",
};

describe("Connected Apps API parsing", () => {
  it("reads the overview and falls back to empty when it is malformed", async () => {
    let { client } = respondWith({
      catalog_mode: "custom",
      allowed_toolkits: ["gmail"],
      can_manage: true,
      team: [],
      personal: [{ id: "c1", toolkit_slug: "gmail", status: "active", connected_at: "t", last_used_at: null }],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "write" }],
    });
    const overview = await client.getAppsOverview();
    expect(overview.catalog_mode).toBe("custom");
    expect(overview.personal[0]?.connected_by).toBeNull();
    expect(overview.agent_apps[0]?.level).toBe("write");

    ({ client } = respondWith({ team: "nope" }));
    expect(await client.getAppsOverview()).toEqual(EMPTY_APPS_OVERVIEW);
  });

  it("maps enum values this client does not know to the cautious ones", async () => {
    const { client } = respondWith({
      catalog_mode: "future_mode",
      team: [],
      personal: [],
      agent_apps: [{ agent_id: "a1", toolkit_slug: "gmail", account_scope: "org", level: "admin" }],
    });
    const overview = await client.getAppsOverview();
    expect(overview.catalog_mode).toBe("curated");
    expect(overview.can_manage).toBe(false);
    expect(overview.agent_apps[0]).toMatchObject({ account_scope: "personal", level: "confirm_writes" });
  });

  it("keeps request details hidden unless the server says otherwise", async () => {
    const { redacted: _redacted, arguments: _args, ...rest } = request;
    const { client } = respondWith({ requests: [{ ...rest, action_class: "transfer_funds" }], next_before: "2026-11-01T00:00:00Z" });
    const page = await client.listAppRequests({ issueId: "i1" });
    expect(page.next_before).toBe("2026-11-01T00:00:00Z");
    expect(page.requests[0]).toMatchObject({ redacted: true, action_class: "send", chat_session_id: null });
  });

  it("sends the history cursor and tolerates a malformed list", async () => {
    const { client, fetchMock } = respondWith({ requests: [{ id: 1 }] });
    const page = await client.listAppRequests({ issueId: "i1", before: "2026-11-01T00:00:00Z" });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain("before=2026-11-01T00%3A00%3A00Z");
    expect(page).toEqual({ requests: [], next_before: null });
  });

  it("returns null from writes whose response is malformed, so callers refetch", async () => {
    let { client } = respondWith({ id: "r1" });
    expect(await client.approveAppRequest("r1")).toBeNull();
    ({ client } = respondWith({ apps: [{ toolkit_slug: 5 }] }));
    expect(await client.updateAgentApps("a1", { apps: [] })).toBeNull();
    ({ client } = respondWith({ rules: "x" }));
    expect(await client.updateAppRules("gmail", "personal", [])).toBeNull();
    ({ client } = respondWith(request));
    expect(await client.rejectAppRequest("r1")).toMatchObject({ id: "r1", redacted: false });
  });

  it("refuses to redirect without a connect URL", async () => {
    let { client } = respondWith({});
    await expect(client.connectApp("personal", "gmail", "/ws/issues/1")).rejects.toThrow(/malformed/i);
    const ok = respondWith({ redirect_url: "https://connect.example.test" });
    client = ok.client;
    expect(await client.connectApp("personal", "gmail", "/ws/issues/1")).toEqual({ redirect_url: "https://connect.example.test" });
    expect(JSON.parse(String(ok.fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ toolkit_slug: "gmail", return_to: "/ws/issues/1" });
  });

  it("parses tools and issue calls, dropping malformed pages", async () => {
    let { client } = respondWith({
      tools: [{ slug: "GMAIL_SEND_EMAIL", name: "Send", auto_class: "send", class: "unknown", overridden: true }],
    });
    expect((await client.listAppTools("gmail")).tools[0]).toMatchObject({ class: "send", description: "" });
    ({ client } = respondWith({ tools: null }));
    expect(await client.listAppTools("gmail")).toEqual({ tools: [] });

    ({ client } = respondWith({
      calls: [{
        id: "c1", agent_id: "a1", account_scope: "team", toolkit_slug: "gmail", tool_slug: "GMAIL_SEND_EMAIL",
        action_class: "send", outcome: "rate_limited", created_at: "t",
      }],
    }));
    expect((await client.listIssueAppCalls("i1")).calls[0]).toMatchObject({ task_id: null, error: null, outcome: "rate_limited" });
    ({ client } = respondWith("oops"));
    expect(await client.listIssueAppCalls("i1")).toEqual({ calls: [] });
  });
});
