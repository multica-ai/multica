// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { AgentTaskSchema } from "./schemas";
afterEach(() => vi.unstubAllGlobals());
const client = new ApiClient("https://api.example.test");
it("does not present malformed wakeup state as an empty list", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify([{ id: "wake", enabled: "false" }])),
      ),
  );
  await expect(client.listIssueWakeups("issue")).rejects.toThrow(
    "Could not load wakeups",
  );
});
it("preserves an empty wakeup list", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("[]")));
  await expect(client.listIssueWakeups("issue")).resolves.toEqual([]);
});
it("does not swallow a disable permission refusal", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response('{"error":"forbidden"}', { status: 403 }),
      ),
  );
  await expect(client.disableIssueWakeup("issue", "wake")).rejects.toThrow();
});

it("rejects malformed wakeup summary counts", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify([{ active_count: "2" }]))),
  );
  await expect(client.listIssueWakeupSummaries()).rejects.toThrow();
});
it("preserves empty summaries", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("[]")));
  await expect(client.listIssueWakeupSummaries()).resolves.toEqual([]);
});

const inventoryFilters = {
  scope: "active",
  kind: "all",
  source: "",
  search: "",
  agent_id: "",
  offset: 0,
  limit: 50,
} as const;
it("rejects malformed workspace inventory rather than hiding ongoing work", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ items: [], total: "0" })),
      ),
  );
  await expect(client.listWorkspaceWakeups(inventoryFilters)).rejects.toThrow(
    "Could not load workspace wakeups",
  );
});
it("preserves an empty page and its inventory counts", async () => {
  const page = {
    items: [],
    total: 101,
    counts: { all: 101, active: 100, disabled: 1, ended: 0 },
    agents: [],
  };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(page)));
  vi.stubGlobal("fetch", fetcher);
  await expect(
    client.listWorkspaceWakeups({
      ...inventoryFilters,
      offset: 150,
      search: "CI & release",
    }),
  ).resolves.toEqual({ ...page, counts: { ...page.counts, paused: 0 } });
  expect(fetcher.mock.calls[0]![0]).toContain("search=CI+%26+release");
  expect(fetcher.mock.calls[0]![0]).toContain("offset=150");
});

it("preserves wakeup origin while accepting old task responses", () => {
  expect(
    AgentTaskSchema.parse({ id: "run", wakeup_id: "wake", status: "deferred" }),
  ).toMatchObject({ wakeup_id: "wake", status: "deferred" });
  expect(AgentTaskSchema.parse({ id: "run" }).wakeup_id).toBeUndefined();
});

it("sends a scoped enable request without rewriting the configuration", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("{}"));
  vi.stubGlobal("fetch", fetcher);
  await client.enableIssueWakeup("issue", "wake", {
    revision: 2,
    rearm: true,
    at: "2099-01-01T00:00:00Z",
  });
  const [url, init] = fetcher.mock.calls[0]!;
  expect(url).toContain("/api/issues/issue/wakeups/wake/enable");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body)).toEqual({
    revision: 2,
    rearm: true,
    at: "2099-01-01T00:00:00Z",
  });
});
it("surfaces a stale enable refusal instead of reporting success", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response('{"error":"wakeup changed"}', { status: 409 }),
      ),
  );
  await expect(
    client.enableIssueWakeup("issue", "wake", { revision: 1 }),
  ).rejects.toThrow();
});

it("preserves actor filters and accepts older responses without them", async () => {
  const rule = {
    id: "wake", issue_id: "issue", agent_id: "agent", agent_name: "Emacs",
    instruction: "wait", kind: "event", mode: "once", event_types: ["comment.created"],
    filter_agent_id: null, filter_task_id: null, interval_seconds: null,
    cron_expression: null, timezone: "UTC", next_fire_at: null, enabled: true,
    disabled_at: null, last_task_id: null, last_error: null,
  };
  for (const fields of [{}, { filter_actor_type: "member", filter_actor_id: "user", filter_actor_name: "Jiayuan" }, { filter_actor_type: "agent", filter_actor_id: null, filter_actor_name: null }]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ ...rule, ...fields }]))));
    await expect(client.listIssueWakeups("issue")).resolves.toEqual([{ ...rule, ...fields }]);
  }
  for (const fields of [{ filter_actor_type: 42 }, { filter_actor_type: "robot" }, { filter_actor_id: 42 }, { filter_actor_name: 42 }]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ ...rule, ...fields }]))));
    await expect(client.listIssueWakeups("issue")).rejects.toThrow("Could not load wakeups");
  }
});

it("edits instructions through the scoped endpoint and propagates conflicts", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetch);
  const input = { instruction: "new", expected_instruction: "old", revision: 2 };
  await client.editIssueWakeupInstruction("issue", "wake", input);
  expect(fetch.mock.calls[0]?.[0]).toContain("/api/issues/issue/wakeups/wake/instruction");
  expect(fetch.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ method: "PATCH", body: JSON.stringify(input) }));
  fetch.mockResolvedValue(new Response('{"error":"conflict"}', { status: 409 }));
  await expect(client.editIssueWakeupInstruction("issue", "wake", input)).rejects.toThrow();
});

it("parses rule deadlines and the creator, tolerating an unknown timeout action", async () => {
  const row = {
    id: "wake", issue_id: "issue", agent_id: "agent", agent_name: "Emacs", instruction: "x",
    kind: "event", mode: "once", event_types: ["comment.created"], filter_agent_id: null,
    filter_task_id: null, interval_seconds: null, cron_expression: null, timezone: "UTC",
    next_fire_at: null, enabled: true, disabled_at: null, last_task_id: null, last_error: null,
    expires_at: "2026-09-27T08:00:00Z", expiry_seconds: 259200, on_timeout: "escalate",
    timed_out_at: null, created_by_agent: true, created_by_name: "Jiayuan",
    source_agent_id: "agent", source_agent_name: "Emacs",
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([row]))));
  const [parsed] = await client.listIssueWakeups("issue");
  expect(parsed).toMatchObject({ expires_at: row.expires_at, expiry_seconds: 259200, on_timeout: null, created_by_agent: true, source_agent_name: "Emacs" });
});
it("creates a wakeup with its full configuration", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("{}", { status: 201 }));
  vi.stubGlobal("fetch", fetcher);
  await client.createIssueWakeup("issue", { agent_id: "agent", instruction: "x", kind: "event", event_types: ["comment.created"], expires_in_seconds: 3600, on_timeout: "wake" });
  const [url, init] = fetcher.mock.calls[0]!;
  expect(String(url)).toContain("/api/issues/issue/wakeups");
  expect(init.method).toBe("POST");
  expect(JSON.parse(init.body)).toMatchObject({ expires_in_seconds: 3600, on_timeout: "wake" });
});
it("rejects malformed system wakeups instead of hiding the rule", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ rule: "child_done", enabled: "yes" }]))));
  await expect(client.listIssueSystemWakeups("issue")).rejects.toThrow("Could not load system wakeups");
});
it("parses system wakeups and falls back on an unknown blocked reason", async () => {
  const rule = { rule: "child_done", enabled: true, instruction: "", staged: true, stage: 1, total: 2, remaining: 1, waiting: ["MUL-2"], target: { type: "agent", id: "a", name: "Emacs" }, blocked: "paused" };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([rule]))));
  // Fields a server may omit get defaults: the rule reads as on and not yet created.
  await expect(client.listIssueSystemWakeups("issue")).resolves.toEqual([{
    ...rule, blocked: "", workspace_default: true, id: "", revision: 0, default_instruction: "", customized: false, paused_reason: null,
  }]);
});
it("reads a member target and a pause, and drops an unknown target", async () => {
  const base = { id: "r", revision: 3, rule: "child_done", enabled: false, instruction: "", default_instruction: "Advance.", customized: true,
    staged: false, stage: null, total: 1, remaining: 1, waiting: [], blocked: "member_assignee", workspace_default: true };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([
    { ...base, paused_reason: "rate", target: { type: "member", id: "u", name: "Jiayuan" } },
    { ...base, paused_reason: "someday", target: { type: "robot", id: "x", name: "?" } },
  ]))));
  const [member, unknown] = await client.listIssueSystemWakeups("issue");
  expect(member).toMatchObject({ paused_reason: "rate", target: { type: "member", name: "Jiayuan" }, customized: true });
  expect(unknown).toMatchObject({ paused_reason: null, target: null });
});
it("reads workspace defaults and rejects a malformed list", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ rule: "child_done", enabled: false, customized: 2 }]))));
  await expect(client.listWorkspaceSystemWakeups()).resolves.toEqual([
    { rule: "child_done", enabled: false, instruction: "", builtin_instruction: "", customized: 2 },
  ]);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ rule: "other" }]))));
  await expect(client.listWorkspaceSystemWakeups()).rejects.toThrow("Could not load system wakeups");
});
it("sends a workspace default change", async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response("[]"));
  vi.stubGlobal("fetch", fetcher);
  await client.updateWorkspaceSystemWakeup("child_done", { enabled: false });
  const [url, init] = fetcher.mock.calls[0]!;
  expect(String(url)).toContain("/api/system-wakeups/child_done");
  expect(init.method).toBe("PUT");
  expect(JSON.parse(init.body)).toEqual({ enabled: false });
});

it("reads conditions, caps and pauses, and drops shapes it does not know", async () => {
  const base = {
    id: "w", issue_id: "i", agent_id: "a", agent_name: "Emacs", instruction: "go", kind: "event", mode: "continuous",
    event_types: ["issue.status_changed"], filter_agent_id: null, filter_task_id: null, interval_seconds: null,
    cron_expression: null, timezone: "UTC", next_fire_at: null, enabled: false, disabled_at: null,
    last_task_id: null, last_error: null,
  };
  const rows = [
    { ...base, condition: { type: "issue_field", field: "status", value: "in_review" }, max_fires: 20, fire_count: 20, paused_reason: "max_fires" },
    { ...base, id: "w2", condition: { type: "future_kind" }, paused_reason: "someday" },
    { ...base, id: "w3", condition: { type: "other_issue", issue_id: "x", state: "done", identifier: "MUL-2" } },
  ];
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(rows))));
  const [known, unknown, other] = await client.listIssueWakeups("i");
  expect(known).toMatchObject({ condition: { field: "status", value: "in_review" }, max_fires: 20, fire_count: 20, paused_reason: "max_fires" });
  expect(unknown).toMatchObject({ condition: null, paused_reason: null });
  expect(other!.condition).toEqual({ type: "other_issue", issue_id: "x", state: "done", identifier: "MUL-2" });
});

it("reads system rule rows in the workspace list", async () => {
  const page = {
    items: [{
      id: "p", issue_id: "p", issue_title: "Parent", issue_identifier: "MUL-1", issue_closed: false, can_manage: true,
      active_runs: 0, task: null, agent_id: null, agent_name: "", instruction: undefined, kind: "event", mode: "continuous",
      event_types: [], filter_agent_id: null, filter_task_id: null, interval_seconds: null, cron_expression: null,
      timezone: "UTC", next_fire_at: null, enabled: true, disabled_at: null, last_task_id: null, last_error: null,
      revision: null, source: "system", rule: "child_done", system_stage: 2, system_remaining: 1, runs_7d: 3,
    }],
    total: 1, counts: { all: 1, active: 1, paused: 0, disabled: 0, ended: 0 }, agents: [],
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(page))));
  const result = await client.listWorkspaceWakeups(inventoryFilters);
  expect(result.items[0]).toMatchObject({ agent_id: "", source: "system", rule: "child_done", system_stage: 2, runs_7d: 3 });
  expect(result.items[0]!.revision).toBeUndefined();
});

it("loads a rule's runs and the paused list", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response(JSON.stringify([
    { id: "r", status: "completed", created_at: "2026-09-24T00:00:00Z", started_at: null, completed_at: null, checkin_note: "ok", triggers: ["time.due"], commented: false },
  ]))).mockResolvedValueOnce(new Response(JSON.stringify([
    { issue_id: "i", id: "w", agent_id: "a", paused_reason: "loop" },
  ]))));
  await expect(client.listIssueWakeupRuns("i", "w")).resolves.toEqual([
    expect.objectContaining({ id: "r", checkin_note: "ok", triggers: ["time.due"], commented: false }),
  ]);
  await expect(client.listPausedWakeups()).resolves.toEqual([{ issue_id: "i", id: "w", agent_id: "a", paused_reason: "loop" }]);
});
