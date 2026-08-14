import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { ApiClient } from "./client";
import { parseWithFallback } from "./schema";

// Helper: stub fetch with a single JSON response. Status defaults to 200.
function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// These tests cover the five failure modes that white-screened the desktop
// app in past incidents. The contract is: a malformed response degrades to
// an empty/safe shape, never throws into React.
describe("ApiClient schema fallback", () => {
  describe("GitHub repository import", () => {
    it("falls back safely when installation or repository responses are malformed", async () => {
      stubFetchJson({ installations: "not-an-array", configured: true });
      const client = new ApiClient("https://api.example.test");
      await expect(client.listGitHubInstallations("ws-1")).resolves.toEqual({
        installations: [],
        configured: false,
        repository_browse_configured: false,
        can_manage: false,
      });

      stubFetchJson({ repositories: [{ id: "wrong-type" }] });
      await expect(
        client.listGitHubInstallationRepositories("ws-1", "installation-1"),
      ).resolves.toEqual({
        repositories: [],
        total_count: 0,
        next_page: null,
      });
    });

    it("adds the allowlisted repository return target to the connect request", async () => {
      stubFetchJson({
        configured: true,
        url: "https://github.com/apps/multica/installations/new",
      });
      const client = new ApiClient("https://api.example.test");

      await client.getGitHubConnectURL("ws-1", "repositories");

      const fetchMock = vi.mocked(fetch);
      expect(fetchMock).toHaveBeenCalledWith(
        "https://api.example.test/api/workspaces/ws-1/github/connect?return_to=repositories",
        expect.any(Object),
      );
    });
  });

  describe("listTimeline", () => {
    it("falls back to an empty array when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toEqual([]);
    });

    it("falls back when the body is not an array", async () => {
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toEqual([]);
    });

    it("accepts a new entry type rather than crashing on enum drift", async () => {
      stubFetchJson([
        {
          type: "future_kind", // not in TS union
          id: "e-1",
          actor_type: "member",
          actor_id: "u-1",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toHaveLength(1);
      expect(entries[0]?.type).toBe("future_kind");
    });

    // Forward-compat: when the server adds a new field to an existing
    // shape, `.loose()` lets it pass through unchanged. Without `.loose()`
    // zod 4 strips it, which would silently break a future TS type that
    // adopts the field — see schemas.ts header comment.
    it("preserves unknown fields the schema didn't list", async () => {
      stubFetchJson([
        {
          type: "comment",
          id: "e-1",
          actor_type: "member",
          actor_id: "u-1",
          created_at: "2026-01-01T00:00:00Z",
          // New server-side field not present in TimelineEntrySchema:
          future_field: { nested: "value" },
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      const entry = entries[0] as unknown as Record<string, unknown>;
      expect(entry.future_field).toEqual({ nested: "value" });
    });
  });

  describe("listIssues", () => {
    it("falls back to an empty list when the response is malformed", async () => {
      // `issues` having the wrong type triggers the fallback. An object
      // with only unexpected keys would *succeed* parsing now (every
      // declared field has a default) and just pass the extras through
      // via `.loose()`, so we use a wrong-type payload here instead.
      stubFetchJson({ issues: "not-an-array", total: 0 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listIssues();
      expect(res).toEqual({ issues: [], total: 0 });
    });
  });

  describe("createIssue", () => {
    // The create modal decides whether to run its label-attach fallback by
    // reading `labels` off the parsed response, and treats a rejection as a
    // failed create (keep the draft, failure toast). So: a valid issue with
    // any labels shape resolves (labels absent → undefined, valid → Label[],
    // malformed → undefined), but a body that isn't a usable issue rejects
    // rather than fabricating a blank "success".
    const validIssue = {
      id: "issue-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "MUL-1",
      title: "Created",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 0,
      stage: null,
      start_date: null,
      due_date: null,
      metadata: {},
      properties: {},
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-01T00:00:00Z",
    };
    const label = {
      id: "label-1",
      workspace_id: "ws-1",
      name: "bug",
      color: "#ef4444",
      created_at: "2025-01-01T00:00:00Z",
      updated_at: "2025-01-01T00:00:00Z",
    };

    it("keeps labels undefined when the backend omits the field (older backend)", async () => {
      stubFetchJson(validIssue, 201);
      const client = new ApiClient("https://api.example.test");
      const issue = await client.createIssue({ title: "Created" });
      expect(issue.id).toBe("issue-1");
      expect(issue.labels).toBeUndefined();
    });

    it("validates a well-formed labels array", async () => {
      stubFetchJson({ ...validIssue, labels: [label] }, 201);
      const client = new ApiClient("https://api.example.test");
      const issue = await client.createIssue({ title: "Created" });
      expect(issue.labels?.map((l) => l.id)).toEqual(["label-1"]);
    });

    it("degrades a null labels field to undefined so the client falls back", async () => {
      stubFetchJson({ ...validIssue, labels: null }, 201);
      const client = new ApiClient("https://api.example.test");
      const issue = await client.createIssue({ title: "Created" });
      // The issue itself still parses; only the malformed labels degrade.
      expect(issue.id).toBe("issue-1");
      expect(issue.labels).toBeUndefined();
    });

    it("degrades a labels array of the wrong element shape to undefined", async () => {
      stubFetchJson({ ...validIssue, labels: [{ nope: true }] }, 201);
      const client = new ApiClient("https://api.example.test");
      const issue = await client.createIssue({ title: "Created" });
      expect(issue.id).toBe("issue-1");
      expect(issue.labels).toBeUndefined();
    });

    it("rejects when the whole response body is not a usable issue (no fake success)", async () => {
      stubFetchJson({ not: "an issue" }, 201);
      const client = new ApiClient("https://api.example.test");
      await expect(client.createIssue({ title: "Created" })).rejects.toThrow();
    });

    it("rejects when the created issue has an empty id", async () => {
      stubFetchJson({ ...validIssue, id: "" }, 201);
      const client = new ApiClient("https://api.example.test");
      await expect(client.createIssue({ title: "Created" })).rejects.toThrow();
    });
  });

  describe("searchIssues", () => {
    it("falls back to an empty result when the response is malformed", async () => {
      stubFetchJson({ issues: "not-an-array", total: 0 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.searchIssues({ q: "bug" });
      expect(res).toEqual({ issues: [], total: 0 });
    });
  });

  describe("searchProjects", () => {
    it("falls back to an empty result when the response is malformed", async () => {
      stubFetchJson({ projects: "not-an-array", total: 0 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.searchProjects({ q: "roadmap" });
      expect(res).toEqual({ projects: [], total: 0 });
    });
  });

  describe("listAutopilots", () => {
    const baseAutopilot = {
      id: "ap-1",
      workspace_id: "ws-1",
      title: "Daily triage",
      description: null,
      assignee_id: "agent-1",
      status: "active",
      execution_mode: "run_only",
      issue_title_template: null,
      created_by_type: "member",
      created_by_id: "user-1",
      last_run_at: null,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-01T00:00:00Z",
    };

    it("falls back to an empty list when the response is malformed", async () => {
      stubFetchJson({ autopilots: "not-an-array", total: 1 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilots();
      expect(res).toEqual({ autopilots: [], total: 0 });
    });

    it("accepts an old-server row without assignee_type or derived fields", async () => {
      // Pre-MUL-2429 servers omit assignee_type; servers older than the
      // list-derived-fields change omit trigger_kinds/next_run_at/
      // last_run_status. Both must parse, not fall back.
      stubFetchJson({ autopilots: [baseAutopilot], total: 1 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilots();
      expect(res.autopilots).toHaveLength(1);
      expect(res.autopilots[0]?.assignee_type).toBe("agent");
      expect(res.autopilots[0]?.trigger_kinds).toBeUndefined();
      expect(res.autopilots[0]?.last_run_status).toBeUndefined();
    });

    it("passes derived fields through and tolerates enum drift", async () => {
      stubFetchJson({
        autopilots: [
          {
            ...baseAutopilot,
            assignee_type: "squad",
            trigger_kinds: ["schedule", "some_future_kind"],
            next_run_at: "2026-06-13T09:00:00Z",
            last_run_status: "some_future_status",
          },
        ],
        total: 1,
      });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilots();
      expect(res.autopilots[0]?.trigger_kinds).toEqual([
        "schedule",
        "some_future_kind",
      ]);
      expect(res.autopilots[0]?.next_run_at).toBe("2026-06-13T09:00:00Z");
      expect(res.autopilots[0]?.last_run_status).toBe("some_future_status");
    });
  });

  describe("listDingTalkInstallations", () => {
    it("falls back to a safe empty shape when the response is malformed", async () => {
      // `installations` with the wrong type triggers the fallback; the panel
      // must not white-screen on `configured`/`install_supported`.
      stubFetchJson({ installations: "not-an-array", configured: true });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listDingTalkInstallations("ws-1");
      expect(res).toEqual({ installations: [], configured: false });
    });

    it("tolerates an old-server row and a missing install_supported flag", async () => {
      stubFetchJson({
        installations: [{ id: "dt-1", status: "active" }],
        configured: true,
        // install_supported omitted (predates the flag) -> undefined, not a crash
        future_field: true,
      });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listDingTalkInstallations("ws-1");
      expect(res.installations).toHaveLength(1);
      expect(res.configured).toBe(true);
      expect(res.install_supported).toBeUndefined();
      expect(res.group_routing_supported).toBeUndefined();
    });

    it("parses the new-server group-routing capability", async () => {
      stubFetchJson({
        installations: [{ id: "dt-1", status: "active" }],
        configured: true,
        install_supported: true,
        group_routing_supported: true,
      });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listDingTalkInstallations("ws-1");
      expect(res.group_routing_supported).toBe(true);
    });
  });

  describe("listDingTalkGroupRoutes", () => {
    it("falls back to an empty list when the response is malformed", async () => {
      stubFetchJson({ routes: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      await expect(client.listDingTalkGroupRoutes("ws-1")).resolves.toEqual({ routes: [] });
    });

    it("defaults fields missing from an older or partial route row", async () => {
      stubFetchJson({ routes: [{ id: "route-1", agent_id: "agent-2" }] });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listDingTalkGroupRoutes("ws-1");
      expect(res.routes[0]).toMatchObject({
        id: "route-1",
        agent_id: "agent-2",
        conversation_title: "",
        installation_id: "",
      });
    });
  });

  describe("updateDingTalkGroupRoute", () => {
    it("falls back safely on a malformed PATCH response and sends the scoped request", async () => {
      stubFetchJson({ id: 42, agent_id: { wrong: "shape" } });
      const client = new ApiClient("https://api.example.test");
      await expect(
        client.updateDingTalkGroupRoute("ws-1", "route-1", { agent_id: "agent-2" }),
      ).resolves.toEqual({
        id: "",
        workspace_id: "",
        installation_id: "",
        conversation_id: "",
        conversation_title: "",
        agent_id: "",
        discovered_at: "",
        updated_at: "",
      });
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        "https://api.example.test/api/workspaces/ws-1/dingtalk/group-routes/route-1",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ agent_id: "agent-2" }),
        }),
      );
    });
  });

  describe("getConfig", () => {
    it("drops malformed daemon setup URLs instead of throwing", async () => {
      stubFetchJson({
        cdn_domain: "cdn.example.com",
        allow_signup: true,
        daemon_server_url: { wrong: "shape" },
        daemon_app_url: 123,
        workspace_creation_disabled: false,
        feature_flags: { composio_mcp_apps: true },
      });
      const client = new ApiClient("https://api.example.test");
      const config = await client.getConfig();
      expect(config.cdn_domain).toBe("cdn.example.com");
      expect(config.allow_signup).toBe(true);
      expect(config.daemon_server_url).toBeUndefined();
      expect(config.daemon_app_url).toBeUndefined();
      expect(config.feature_flags?.composio_mcp_apps).toBe(true);
    });
  });

  describe("listGroupedIssues", () => {
    it("falls back to empty groups when the response is malformed", async () => {
      stubFetchJson({ groups: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listGroupedIssues({ group_by: "assignee" });
      expect(res).toEqual({ groups: [] });
    });
  });

  describe("listComments", () => {
    it("returns [] when the response is not an array", async () => {
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const comments = await client.listComments("issue-1");
      expect(comments).toEqual([]);
    });
  });

  describe("previewCommentTriggers", () => {
    it("returns an empty agent list when the response is malformed", async () => {
      stubFetchJson({ agents: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const preview = await client.previewCommentTriggers("issue-1", "hello");
      expect(preview).toEqual({ agents: [] });
    });
  });

  describe("listIssueSubscribers", () => {
    it("returns [] when the response is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const subs = await client.listIssueSubscribers("issue-1");
      expect(subs).toEqual([]);
    });
  });

  describe("listChildIssues", () => {
    it("returns { issues: [] } when the issues field is missing", async () => {
      stubFetchJson({});
      const client = new ApiClient("https://api.example.test");
      const res = await client.listChildIssues("issue-1");
      expect(res).toEqual({ issues: [] });
    });
  });

  describe("listAutopilotDeliveries", () => {
    it("falls back to an empty list when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilotDeliveries("ap-1");
      expect(res).toEqual({ deliveries: [], total: 0 });
    });

    it("falls back to an empty list when `deliveries` is not an array", async () => {
      stubFetchJson({ deliveries: "not-an-array", total: 0 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilotDeliveries("ap-1");
      expect(res).toEqual({ deliveries: [], total: 0 });
    });

    it("accepts an unknown future status value rather than dropping the row", async () => {
      // Server-side enum drift (e.g. new `quarantined` state). The list
      // must still surface the row; downstream UI code's `default` arm
      // handles unknown values with a generic visual.
      stubFetchJson({
        deliveries: [
          {
            id: "d-1",
            workspace_id: "ws-1",
            autopilot_id: "ap-1",
            trigger_id: "t-1",
            provider: "github",
            event: "pull_request.opened",
            dedupe_key: "abc",
            dedupe_source: "x-github-delivery",
            signature_status: "valid",
            status: "quarantined",
            attempt_count: 1,
            content_type: "application/json",
            response_status: 200,
            autopilot_run_id: null,
            replayed_from_delivery_id: null,
            error: null,
            received_at: "2026-01-01T00:00:00Z",
            last_attempt_at: "2026-01-01T00:00:00Z",
            created_at: "2026-01-01T00:00:00Z",
          },
        ],
        total: 1,
      });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listAutopilotDeliveries("ap-1");
      expect(res.deliveries).toHaveLength(1);
      expect(res.deliveries[0]?.status).toBe("quarantined");
      expect(res.deliveries[0]?.dispatch_attempts).toBe(0);
      expect(res.deliveries[0]?.available_at).toBe("");
    });
  });

  describe("getAutopilotDelivery", () => {
    it("falls back to a placeholder carrying the requested id", async () => {
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const detail = await client.getAutopilotDelivery("ap-1", "d-1");
      expect(detail.id).toBe("d-1");
      expect(detail.autopilot_id).toBe("ap-1");
    });
  });

  describe("listAgentBuilderSessions", () => {
    it("falls back to no drafts when the response is malformed", async () => {
      stubFetchJson({ unexpected: "shape" });
      const client = new ApiClient("https://api.example.test");
      expect(await client.listAgentBuilderSessions()).toEqual([]);
    });

    // Losing the whole row would lose the conversation. A draft missing its
    // runtime degrades to "let the user pick one" instead.
    it("keeps a draft whose runtime the server omitted", async () => {
      stubFetchJson({
        sessions: [{ session_id: "s-1", title: "Create an agent" }],
      });
      const client = new ApiClient("https://api.example.test");
      const sessions = await client.listAgentBuilderSessions();
      expect(sessions).toHaveLength(1);
      expect(sessions[0]?.session_id).toBe("s-1");
      expect(sessions[0]?.runtime_id).toBe("");
    });

    // An installed desktop build can outrun a self-hosted backend. Without this
    // the Agents page would error instead of simply offering no drafts.
    it("treats a 404 from an older backend as no drafts", async () => {
      stubFetchJson({ error: "not found" }, 404);
      const client = new ApiClient("https://api.example.test");
      expect(await client.listAgentBuilderSessions()).toEqual([]);
    });
  });

  describe("cronPreview", () => {
    it("returns the parsed next runs", async () => {
      stubFetchJson({
        next_runs: ["2026-07-14T01:00:00Z", "2026-07-14T03:00:00Z"],
      });
      const client = new ApiClient("https://api.example.test");
      const res = await client.cronPreview({
        expr: "0 9-21/2 * * *",
        tz: "Asia/Shanghai",
      });
      expect(res).toEqual({
        next_runs: ["2026-07-14T01:00:00Z", "2026-07-14T03:00:00Z"],
      });
    });

    it("URL-encodes the expression and timezone", async () => {
      stubFetchJson({ next_runs: [] });
      const client = new ApiClient("https://api.example.test");
      await client.cronPreview({ expr: "0 9-21/2 * * 2-4", tz: "Asia/Shanghai" });
      const url = String(vi.mocked(fetch).mock.calls[0]?.[0]);
      expect(url).toContain("/api/autopilots/cron-preview?");
      expect(url).toContain("expr=0+9-21%2F2+*+*+2-4");
      expect(url).toContain("tz=Asia%2FShanghai");
    });

    it("returns an empty list verbatim when the expression never fires", async () => {
      stubFetchJson({ next_runs: [] });
      const client = new ApiClient("https://api.example.test");
      const res = await client.cronPreview({ expr: "0 0 30 2 *", tz: "UTC" });
      expect(res).toEqual({ next_runs: [] });
    });

    it("falls back to null when the response is malformed", async () => {
      // null, not [] — the caller must be able to tell "unreadable response"
      // apart from "this expression never fires".
      stubFetchJson({ next_runs: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const res = await client.cronPreview({ expr: "0 9 * * *", tz: "UTC" });
      expect(res).toEqual({ next_runs: null });
    });

    it("falls back to null when the field is missing entirely", async () => {
      stubFetchJson({ runs: ["2026-07-14T01:00:00Z"] });
      const client = new ApiClient("https://api.example.test");
      const res = await client.cronPreview({ expr: "0 9 * * *", tz: "UTC" });
      expect(res).toEqual({ next_runs: null });
    });
  });

  describe("cloud billing", () => {
    it("falls back to an empty balance when the response is malformed", async () => {
      stubFetchJson({ balance_micro: "not-a-number" });
      const client = new ApiClient("https://api.example.test");

      await expect(client.getCloudBillingBalance()).resolves.toEqual({
        owner_id: "",
        balance_micro: 0,
        balance_credit: 0,
        updated_at: "",
      });
    });

    it("falls back to an empty transactions page when items are malformed", async () => {
      stubFetchJson({ items: "not-an-array", total: 1, page: 1, page_size: 20 });
      const client = new ApiClient("https://api.example.test");

      await expect(client.listCloudBillingTransactions()).resolves.toEqual({
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
      });
    });

    it("falls back to an empty batches page when items are malformed", async () => {
      stubFetchJson({ items: "not-an-array", total: 1, page: 1, page_size: 20 });
      const client = new ApiClient("https://api.example.test");

      await expect(client.listCloudBillingBatches()).resolves.toEqual({
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
      });
    });

    it("falls back to an empty top-ups page when items are malformed", async () => {
      stubFetchJson({ items: "not-an-array", total: 1, page: 1, page_size: 20 });
      const client = new ApiClient("https://api.example.test");

      await expect(client.listCloudBillingTopups()).resolves.toEqual({
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
      });
    });

    it("falls back to no price tiers when the response is not an array", async () => {
      stubFetchJson({ tiers: [] });
      const client = new ApiClient("https://api.example.test");

      await expect(client.listCloudBillingPriceTiers()).resolves.toEqual([]);
    });

    it("falls back to an empty checkout session when the response is malformed", async () => {
      stubFetchJson({ order_id: 123 });
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.createCloudBillingCheckoutSession({ tier_id: "starter" }),
      ).resolves.toEqual({
        order_id: "",
        session_id: "",
        url: "",
      });
    });

    it("falls back to a pending checkout status when the response is malformed", async () => {
      stubFetchJson({ status: 123 });
      const client = new ApiClient("https://api.example.test");

      await expect(client.getCloudBillingCheckoutSession("cs_test")).resolves.toEqual({
        order_id: "",
        status: "pending",
        amount_cents: 0,
        credits: 0,
        bonus_credits: 0,
        currency: "usd",
        tier_id: "",
      });
    });

    it("falls back to an empty portal URL when the response is malformed", async () => {
      stubFetchJson({ url: 123 });
      const client = new ApiClient("https://api.example.test");

      await expect(client.createCloudBillingPortalSession()).resolves.toEqual({
        url: "",
      });
    });

    it("parses workspace entitlements into camelCase without fabricating Free", async () => {
      stubFetchJson({
        workspace_id: "workspace-1",
        plan: "pro",
        status: "active",
        seats: 4,
        issue_window: null,
        autopilot_runs: null,
        current_period_end: "2026-09-13T00:00:00Z",
        snapshot_expires_at: null,
        version: 7,
      });
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.getWorkspaceSubscriptionEntitlements(),
      ).resolves.toEqual({
        workspaceId: "workspace-1",
        plan: "pro",
        status: "active",
        seats: 4,
        issueWindow: null,
        autopilotRuns: null,
        currentPeriodEnd: "2026-09-13T00:00:00Z",
        snapshotExpiresAt: null,
        version: 7,
      });

      stubFetchJson({ plan: "free", seats: "unknown" });
      await expect(client.getWorkspaceSubscriptionEntitlements()).resolves.toBeNull();
    });

    it("accepts an empty workspace entitlement snapshot", async () => {
      stubFetchJson({
        workspace_id: "workspace-1",
        plan: "free",
        status: "inactive",
        seats: 0,
        issue_window: 1000,
        autopilot_runs: 100,
        snapshot_expires_at: null,
        version: 0,
      });
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.getWorkspaceSubscriptionEntitlements(),
      ).resolves.toMatchObject({ seats: 0, plan: "free" });
    });

    it("sends the Checkout idempotency key in the header and body", async () => {
      stubFetchJson(
        {
          request_id: "request-1",
          session_id: "cs_test_1",
          url: "https://checkout.stripe.com/test-session",
        },
        201,
      );
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.createWorkspaceSubscriptionCheckout({
          interval: "year",
          idempotencyKey: "checkout-intent-1",
        }),
      ).resolves.toEqual({
        requestId: "request-1",
        sessionId: "cs_test_1",
        url: "https://checkout.stripe.com/test-session",
      });

      const fetchMock = vi.mocked(fetch);
      const init = fetchMock.mock.calls[0]?.[1] as RequestInit & {
        headers: Record<string, string>;
      };
      expect(init.headers["Idempotency-Key"]).toBe("checkout-intent-1");
      expect(JSON.parse(String(init.body))).toEqual({
        interval: "year",
        idempotency_key: "checkout-intent-1",
      });
    });

    it("rejects unreadable or non-HTTPS Stripe URLs at the schema boundary", async () => {
      stubFetchJson(
        {
          request_id: "request-1",
          session_id: "cs_test_1",
          url: "javascript:alert(1)",
        },
        201,
      );
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.createWorkspaceSubscriptionCheckout({
          interval: "month",
          idempotencyKey: "checkout-intent-1",
        }),
      ).resolves.toBeNull();

      stubFetchJson({ url: 123 });
      await expect(
        client.createWorkspaceSubscriptionPortal("portal-intent-1"),
      ).resolves.toBeNull();
    });

    it("parses seat reconciliation into camelCase", async () => {
      stubFetchJson({
        workspace_id: "workspace-1",
        billed_seats: 5,
        actual_seats: 4,
        action: "scheduled_decrease",
      });
      const client = new ApiClient("https://api.example.test");

      await expect(
        client.reconcileWorkspaceSubscriptionSeats(),
      ).resolves.toEqual({
        workspaceId: "workspace-1",
        billedSeats: 5,
        actualSeats: 4,
        action: "scheduled_decrease",
      });
    });
  });
});

// Direct tests for the helper, decoupled from any specific endpoint —
// guards against an endpoint refactor masking a regression in the helper.
describe("parseWithFallback", () => {
  const opts = { endpoint: "TEST /unit" };

  it("returns parsed data on success", () => {
    const schema = z.object({ id: z.string() });
    const out = parseWithFallback({ id: "x" }, schema, { id: "fallback" }, opts);
    expect(out).toEqual({ id: "x" });
  });

  it("returns the fallback when validation fails", () => {
    const schema = z.object({ id: z.string() });
    const fallback = { id: "fallback" };
    const out = parseWithFallback({ id: 123 }, schema, fallback, opts);
    expect(out).toBe(fallback);
  });

  it("returns the fallback when data is null", () => {
    const schema = z.object({ id: z.string() });
    const fallback = { id: "fallback" };
    const out = parseWithFallback(null, schema, fallback, opts);
    expect(out).toBe(fallback);
  });
});

// Workspace subscription reads carry a specific hazard the wallet schemas do
// not: the fallback for a paid workspace must never be a shape that reads as
// Free. An older cloud, a 503, or a renamed field has to surface as "unknown"
// so the UI shows "unavailable" instead of quietly downgrading a paying team.
describe("workspace subscription contract", () => {
  const entitlement = {
    workspace_id: "11111111-1111-1111-1111-111111111111",
    plan: "pro",
    status: "active",
    seats: 3,
    issue_window: null,
    autopilot_runs: null,
    current_period_end: "2026-09-01T00:00:00Z",
    snapshot_expires_at: null,
    version: 7,
  };

  it("maps a full summary to camelCase without inventing values", async () => {
    stubFetchJson({
      entitlement,
      billing_interval: "year",
      actual_seats: 3,
      billed_seats: 5,
      pending_seat_quantity: 3,
      cancel_at_period_end: true,
      grace_until: "2026-09-08T00:00:00Z",
      has_stripe_customer: true,
    });
    const client = new ApiClient("https://api.example.test");
    const summary = await client.getWorkspaceSubscriptionSummary();

    expect(summary).not.toBeNull();
    expect(summary?.entitlement.plan).toBe("pro");
    expect(summary?.entitlement.version).toBe(7);
    expect(summary?.billingInterval).toBe("year");
    expect(summary?.billedSeats).toBe(5);
    expect(summary?.pendingSeatQuantity).toBe(3);
    expect(summary?.cancelAtPeriodEnd).toBe(true);
    expect(summary?.graceUntil).toBe("2026-09-08T00:00:00Z");
    expect(summary?.hasStripeCustomer).toBe(true);
  });

  it("keeps a paid summary readable when optional fields are absent", async () => {
    // A cloud that predates the optional seat/cancellation fields still has to
    // produce a usable Pro summary rather than failing the whole read.
    stubFetchJson({ entitlement, actual_seats: 3 });
    const client = new ApiClient("https://api.example.test");
    const summary = await client.getWorkspaceSubscriptionSummary();

    expect(summary?.entitlement.plan).toBe("pro");
    expect(summary?.billingInterval).toBeNull();
    expect(summary?.billedSeats).toBeNull();
    expect(summary?.pendingSeatQuantity).toBeNull();
    expect(summary?.cancelAtPeriodEnd).toBe(false);
    expect(summary?.graceUntil).toBeNull();
    // Absent means "no Stripe customer known", which is the safe reading: the
    // caller hides Portal rather than offering a control that would 404.
    expect(summary?.hasStripeCustomer).toBe(false);
  });

  it("preserves unknown plans and statuses instead of coercing them", async () => {
    stubFetchJson({
      entitlement: { ...entitlement, plan: "business", status: "paused" },
      actual_seats: 9,
    });
    const client = new ApiClient("https://api.example.test");
    const summary = await client.getWorkspaceSubscriptionSummary();

    expect(summary?.entitlement.plan).toBe("business");
    expect(summary?.entitlement.status).toBe("paused");
  });

  it("returns null — never a Free-looking shape — for a malformed summary", async () => {
    stubFetchJson({ entitlement: { plan: "pro" }, actual_seats: "many" });
    const client = new ApiClient("https://api.example.test");
    expect(await client.getWorkspaceSubscriptionSummary()).toBeNull();
  });

  it("throws ApiError for a non-2xx summary so the caller reports unavailable", async () => {
    // An older cloud without the route, a 403, or a 503 never reaches schema
    // parsing: fetch rejects first and a React Query caller sees isError. What
    // matters for both paths is the same — no snapshot is produced, so nothing
    // can be mistaken for a Free workspace.
    for (const status of [404, 503]) {
      stubFetchJson({ error: "unavailable" }, status);
      const client = new ApiClient("https://api.example.test");
      await expect(client.getWorkspaceSubscriptionSummary()).rejects.toThrow();
    }
  });

  it("returns null for a 2xx body that does not match the contract", async () => {
    // The other half of the contract: a response that arrives successfully but
    // does not conform degrades to null rather than throwing into React.
    stubFetchJson({ unexpected: "shape" });
    const client = new ApiClient("https://api.example.test");
    expect(await client.getWorkspaceSubscriptionSummary()).toBeNull();
  });

  it("accepts additive cloud fields on summary and prices", async () => {
    stubFetchJson({
      entitlement: { ...entitlement, trial_ends_at: "2026-10-01T00:00:00Z" },
      actual_seats: 3,
      future_field: { nested: true },
    });
    const client = new ApiClient("https://api.example.test");
    expect(
      (await client.getWorkspaceSubscriptionSummary())?.entitlement.plan,
    ).toBe("pro");
  });

  it("maps validated prices and rejects a non-positive amount", async () => {
    stubFetchJson({
      month: { currency: "usd", unit_amount: 2000, interval: "month", interval_count: 1 },
      year: { currency: "usd", unit_amount: 20000, interval: "year", interval_count: 1 },
    });
    const client = new ApiClient("https://api.example.test");
    const prices = await client.getWorkspaceSubscriptionPrices();
    expect(prices?.month.unitAmount).toBe(2000);
    expect(prices?.year.interval).toBe("year");

    // A zero or missing amount must not be rendered next to a purchase button.
    stubFetchJson({
      month: { currency: "usd", unit_amount: 0, interval: "month", interval_count: 1 },
      year: { currency: "usd", unit_amount: 20000, interval: "year", interval_count: 1 },
    });
    expect(await client.getWorkspaceSubscriptionPrices()).toBeNull();
  });

  it("rejects a prices response whose interval does not match its slot", async () => {
    const client = new ApiClient("https://api.example.test");

    // An interval outside the contract.
    stubFetchJson({
      month: { currency: "usd", unit_amount: 2000, interval: "month", interval_count: 1 },
      year: { currency: "usd", unit_amount: 20000, interval: "week", interval_count: 1 },
    });
    expect(await client.getWorkspaceSubscriptionPrices()).toBeNull();

    // Valid intervals in the wrong slots. Accepting this would let the UI quote
    // a yearly amount as the monthly price.
    stubFetchJson({
      month: { currency: "usd", unit_amount: 20000, interval: "year", interval_count: 1 },
      year: { currency: "usd", unit_amount: 2000, interval: "month", interval_count: 1 },
    });
    expect(await client.getWorkspaceSubscriptionPrices()).toBeNull();
  });

  it("rejects a prices response whose interval count is not one", async () => {
    stubFetchJson({
      month: { currency: "usd", unit_amount: 2000, interval: "month", interval_count: 3 },
      year: { currency: "usd", unit_amount: 20000, interval: "year", interval_count: 1 },
    });
    const client = new ApiClient("https://api.example.test");

    expect(await client.getWorkspaceSubscriptionPrices()).toBeNull();
  });
});
