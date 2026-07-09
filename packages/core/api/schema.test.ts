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

  describe("agents", () => {
    it("listAgents falls back to [] when the response is malformed", async () => {
      stubFetchJson({ agents: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const agents = await client.listAgents();
      expect(agents).toEqual([]);
    });

    it("getAgent redacts legacy runtime_id and tolerates malformed runtime fields", async () => {
      stubFetchJson({
        id: "agent-1",
        workspace_id: "workspace-1",
        runtime_id: "runtime-private",
        runtime_provider: 42,
        runtime_profile_id: null,
        name: "Agent",
        description: "",
        instructions: "",
        avatar_url: null,
        runtime_mode: "local",
        runtime_config: {},
        custom_args: [],
        visibility: "workspace",
        status: "idle",
        max_concurrent_tasks: 1,
        model: "",
        owner_id: null,
        skills: [],
        created_at: "2026-06-20T00:00:00Z",
        updated_at: "2026-06-20T00:00:00Z",
        archived_at: null,
        archived_by: null,
      });
      const client = new ApiClient("https://api.example.test");
      const agent = await client.getAgent("agent-1");
      expect(agent.id).toBe("agent-1");
      expect(agent.runtime_id).toBeNull();
      expect(agent.runtime_provider).toBe("codex");
      expect(agent.runtime_profile_id).toBeNull();
    });
  });

  describe("listRuntimes", () => {
    it("falls back to [] when the response is malformed", async () => {
      stubFetchJson({ runtimes: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const runtimes = await client.listRuntimes({ owner: "me" });
      expect(runtimes).toEqual([]);
    });

    it("defaults malformed runtime metadata to a safe owner-only shape", async () => {
      stubFetchJson([
        {
          id: "runtime-1",
          workspace_id: "workspace-1",
          daemon_id: null,
          name: "Runtime",
          runtime_mode: 123,
          provider: "codex",
          launch_header: null,
          status: "online",
          device_info: 42,
          metadata: "not-an-object",
          owner_id: null,
          visibility: "public",
          profile_id: 99,
          last_seen_at: null,
          created_at: "2026-06-20T00:00:00Z",
          updated_at: "2026-06-20T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const runtimes = await client.listRuntimes({ owner: "me" });
      expect(runtimes).toHaveLength(1);
      expect(runtimes[0]?.runtime_mode).toBe("local");
      expect(runtimes[0]?.launch_header).toBe("");
      expect(runtimes[0]?.device_info).toBe("");
      expect(runtimes[0]?.metadata).toEqual({});
      expect(runtimes[0]?.profile_id).toBeNull();
    });
  });

  describe("agent task audit responses", () => {
    it("redacts runtime_id from workspace task snapshots", async () => {
      stubFetchJson([
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-private",
          connection_credentials: { token: "secret" },
          daemon_operation: { runtime_id: "runtime-private" },
          runtime_detail_url: "/api/runtimes/runtime-private",
          issue_id: "issue-1",
          status: "running",
          priority: 0,
          dispatched_at: null,
          started_at: null,
          completed_at: null,
          result: null,
          error: null,
          created_at: "2026-06-20T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tasks = await client.getAgentTaskSnapshot();
      expect(tasks).toHaveLength(1);
      expect(tasks[0]?.runtime_id).toBe("");
      expect(tasks[0]?.id).toBe("task-1");
      expect(tasks[0]?.status).toBe("running");
      expect(tasks[0]?.execution_mode).toBe("normal");
      const rawTask = tasks[0] as unknown as Record<string, unknown>;
      expect("connection_credentials" in rawTask).toBe(false);
      expect("daemon_operation" in rawTask).toBe(false);
      expect("runtime_detail_url" in rawTask).toBe(false);
    });

    it("preserves task execution mode when present", async () => {
      stubFetchJson([
        {
          id: "task-goal",
          agent_id: "agent-1",
          runtime_id: "runtime-private",
          issue_id: "issue-1",
          execution_mode: "goal",
          status: "queued",
          priority: 0,
          dispatched_at: null,
          started_at: null,
          completed_at: null,
          result: null,
          error: null,
          created_at: "2026-06-20T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tasks = await client.getAgentTaskSnapshot();
      expect(tasks[0]?.execution_mode).toBe("goal");
    });

    it("redacts runtime fields from issue task history", async () => {
      stubFetchJson([
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-private",
          connection_credentials: { token: "secret" },
          daemon_operation: { runtime_id: "runtime-private" },
          runtime_selector_option: { id: "runtime-private" },
          issue_id: "issue-1",
          status: "completed",
          priority: 0,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-06-20T00:00:00Z",
          result: null,
          error: null,
          created_at: "2026-06-20T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tasks = await client.listTasksByIssue("issue-1");
      expect(tasks[0]?.runtime_id).toBe("");
      const rawTask = tasks[0] as unknown as Record<string, unknown>;
      expect("connection_credentials" in rawTask).toBe(false);
      expect("daemon_operation" in rawTask).toBe(false);
      expect("runtime_selector_option" in rawTask).toBe(false);
    });

    it("redacts nested runtime fields from task audit results", async () => {
      stubFetchJson([
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-private",
          issue_id: "issue-1",
          status: "completed",
          priority: 0,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-06-20T00:00:00Z",
          result: {
            summary: "finished",
            runtime_id: "runtime-private",
            runtimeId: "runtime-private",
            runtime_detail_url: "/api/runtimes/runtime-private",
            workDir: "/Users/alice/.multica/workspaces/ws/task/workdir",
            connectionCredentials: { token: "secret" },
            steps: [
              { name: "safe" },
              {
                name: "unsafe",
                daemon_operation_params: { runtime_id: "runtime-private" },
                daemonOperationParams: { runtimeId: "runtime-private" },
              },
            ],
          },
          error: null,
          created_at: "2026-06-20T00:00:00Z",
          work_dir: "/Users/alice/.multica/workspaces/ws/task/workdir",
          prior_work_dir: "/Users/alice/.multica/workspaces/ws/old/workdir",
          relative_work_dir: "ws/task/workdir",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tasks = await client.listTasksByIssue("issue-1");
      const rawTask = tasks[0] as unknown as Record<string, unknown>;
      expect("work_dir" in rawTask).toBe(false);
      expect("prior_work_dir" in rawTask).toBe(false);
      expect(tasks[0]?.relative_work_dir).toBe("ws/task/workdir");
      const result = tasks[0]?.result as Record<string, unknown>;
      expect(result.summary).toBe("finished");
      expect("runtime_id" in result).toBe(false);
      expect("runtimeId" in result).toBe(false);
      expect("runtime_detail_url" in result).toBe(false);
      expect("workDir" in result).toBe(false);
      expect("connectionCredentials" in result).toBe(false);
      const steps = result.steps as Array<Record<string, unknown>>;
      expect(steps[0]?.name).toBe("safe");
      expect(steps[1]?.name).toBe("unsafe");
      expect("daemon_operation_params" in steps[1]!).toBe(false);
      expect("daemonOperationParams" in steps[1]!).toBe(false);
    });

    it("redacts runtime fields from issue task cancellation responses", async () => {
      stubFetchJson({
        id: "task-1",
        agent_id: "agent-1",
        runtime_id: "runtime-private",
        connection_credentials: { token: "secret" },
        daemon_operation: { runtime_id: "runtime-private" },
        runtime_detail_url: "/api/runtimes/runtime-private",
        issue_id: "issue-1",
        status: "cancelled",
        priority: 0,
        dispatched_at: null,
        started_at: null,
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-06-20T00:00:00Z",
      });
      const client = new ApiClient("https://api.example.test");
      const task = await client.cancelTask("issue-1", "task-1");
      expect(task.runtime_id).toBe("");
      const rawTask = task as unknown as Record<string, unknown>;
      expect("connection_credentials" in rawTask).toBe(false);
      expect("daemon_operation" in rawTask).toBe(false);
      expect("runtime_detail_url" in rawTask).toBe(false);
    });

    it("redacts runtime fields from rerun responses", async () => {
      stubFetchJson({
        id: "task-2",
        agent_id: "agent-1",
        runtime_id: "runtime-private",
        connection_credentials: { token: "secret" },
        daemon_operation_params: { runtime_id: "runtime-private" },
        runtime_call_url: "/api/daemon/runtimes/runtime-private/tasks/claim",
        issue_id: "issue-1",
        status: "queued",
        priority: 0,
        dispatched_at: null,
        started_at: null,
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-06-20T00:00:00Z",
      });
      const client = new ApiClient("https://api.example.test");
      const task = await client.rerunIssue("issue-1", "task-1");
      expect(task.runtime_id).toBe("");
      const rawTask = task as unknown as Record<string, unknown>;
      expect("connection_credentials" in rawTask).toBe(false);
      expect("daemon_operation_params" in rawTask).toBe(false);
      expect("runtime_call_url" in rawTask).toBe(false);
    });

    it("redacts runtime invocation fields from task message inputs", async () => {
      stubFetchJson([
        {
          task_id: "task-1",
          issue_id: "issue-1",
          seq: 1,
          type: "tool_use",
          input: {
            path: "README.md",
            runtime_id: "runtime-private",
            connection_credentials: { token: "secret" },
            daemon_operation: { claim: true },
            runtime_call_url: "/api/daemon/runtimes/runtime-private/tasks/claim",
            nested: {
              keep: "safe",
              runtime_id: "runtime-private",
            },
          },
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const messages = await client.listTaskMessages("task-1");
      const input = messages[0]?.input ?? {};
      expect(input.path).toBe("README.md");
      expect("runtime_id" in input).toBe(false);
      expect("connection_credentials" in input).toBe(false);
      expect("daemon_operation" in input).toBe(false);
      expect("runtime_call_url" in input).toBe(false);
      const nested = input.nested as Record<string, unknown>;
      expect(nested.keep).toBe("safe");
      expect("runtime_id" in nested).toBe(false);
    });

    it("falls back to [] when workspace task snapshot is malformed", async () => {
      stubFetchJson({ tasks: "not-an-array" });
      const client = new ApiClient("https://api.example.test");
      const tasks = await client.getAgentTaskSnapshot();
      expect(tasks).toEqual([]);
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

  describe("getConfig", () => {
    it("drops malformed daemon setup URLs instead of throwing", async () => {
      stubFetchJson({
        cdn_domain: "cdn.example.com",
        allow_signup: true,
        daemon_server_url: { wrong: "shape" },
        daemon_app_url: 123,
        workspace_creation_disabled: false,
      });
      const client = new ApiClient("https://api.example.test");
      const config = await client.getConfig();
      expect(config.cdn_domain).toBe("cdn.example.com");
      expect(config.allow_signup).toBe(true);
      expect(config.daemon_server_url).toBeUndefined();
      expect(config.daemon_app_url).toBeUndefined();
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

  // Agent template catalog is hit by the desktop create-agent picker.
  // Installed desktop builds outlive any given server, so the shape MUST
  // survive future field renames / wrapping without crashing. Each test
  // here mirrors a concrete future drift we want to absorb.
  describe("listAgentTemplates", () => {
    it("falls back to [] when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toEqual([]);
    });

    it("defaults skills to [] when the field is missing from a template", async () => {
      // Future server: drops `skills` because the picker no longer reads
      // them. Picker code calls `template.skills.length` — must not throw.
      stubFetchJson([{ slug: "x", name: "X" }]);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toHaveLength(1);
      expect(tmpls[0]?.skills).toEqual([]);
    });

    it("accepts the bare-array shape (current contract)", async () => {
      stubFetchJson([
        { slug: "a", name: "A", description: "", skills: [] },
        { slug: "b", name: "B", description: "", skills: [] },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls.map((t) => t.slug)).toEqual(["a", "b"]);
    });

    it("accepts a future {templates: [...]} envelope without breaking", async () => {
      // Server migrates to a paginated envelope. We unwrap so the picker
      // keeps working on the older bare-array consumer.
      stubFetchJson({
        templates: [{ slug: "a", name: "A", description: "", skills: [] }],
        total: 1,
      });
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toHaveLength(1);
      expect(tmpls[0]?.slug).toBe("a");
    });
  });

  describe("getAgentTemplate", () => {
    it("falls back to a minimal record carrying the requested slug", async () => {
      // Slug is part of the URL the user clicked — the fallback round-
      // trips it so the page header still makes sense after a parse miss.
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const detail = await client.getAgentTemplate("code-reviewer");
      expect(detail.slug).toBe("code-reviewer");
      expect(detail.skills).toEqual([]);
      expect(detail.instructions).toBe("");
    });

    it("defaults instructions to '' when the field is missing", async () => {
      stubFetchJson({
        slug: "code-reviewer",
        name: "Code Reviewer",
        description: "",
        skills: [],
      });
      const client = new ApiClient("https://api.example.test");
      const detail = await client.getAgentTemplate("code-reviewer");
      expect(detail.instructions).toBe("");
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

  describe("createAgentFromTemplate", () => {
    it("falls back to an empty agent when the response is malformed", async () => {
      // The agent was created server-side even though the client can't
      // parse the response — UI code reads `agent.id === ""` and skips
      // the navigation step rather than landing on `/agents/`.
      stubFetchJson({ unexpected: "shape" });
      const client = new ApiClient("https://api.example.test");
      const resp = await client.createAgentFromTemplate({
        template_slug: "x",
        name: "X",
        runtime_id: "rt-1",
      });
      expect(resp.agent.id).toBe("");
      expect(resp.imported_skill_ids).toEqual([]);
      expect(resp.reused_skill_ids).toEqual([]);
    });

    it("defaults imported_skill_ids / reused_skill_ids to [] when missing", async () => {
      stubFetchJson({ agent: { id: "agent-1" } });
      const client = new ApiClient("https://api.example.test");
      const resp = await client.createAgentFromTemplate({
        template_slug: "x",
        name: "X",
        runtime_id: "rt-1",
      });
      expect(resp.agent.id).toBe("agent-1");
      expect(resp.imported_skill_ids).toEqual([]);
      expect(resp.reused_skill_ids).toEqual([]);
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
