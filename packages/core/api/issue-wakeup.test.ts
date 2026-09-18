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
