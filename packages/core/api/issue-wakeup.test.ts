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
  ).resolves.toEqual(page);
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
