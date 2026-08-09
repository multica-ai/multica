import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const VALID_MIGRATE_BODY = {
  target_runtime_id: "runtime-b",
  dry_run: false,
  migrated: [
    {
      agent_id: "agent-1",
      name: "Lambda",
      cleared_model: "claude-opus-4",
      cleared_thinking_level: "high",
    },
  ],
  skipped: [{ agent_id: "agent-2", name: "Mu", reason: "forbidden" }],
  tasks_migrated: 3,
  tasks_staying_active: 1,
};

describe("ApiClient.migrateAgentsToRuntime", () => {
  it("posts the agent set to the target runtime and returns the server's report", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(VALID_MIGRATE_BODY));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.migrateAgentsToRuntime("runtime-b", {
      agent_ids: ["agent-1", "agent-2"],
      expected_source_runtime_id: "runtime-a",
    });

    const call = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(call[0]).toContain("/api/runtimes/runtime-b/migrate-agents");
    expect(call[1].method).toBe("POST");
    expect(JSON.parse(String(call[1].body))).toEqual({
      agent_ids: ["agent-1", "agent-2"],
      expected_source_runtime_id: "runtime-a",
    });
    expect(result.tasks_migrated).toBe(3);
    expect(result.migrated[0]?.cleared_model).toBe("claude-opus-4");
    expect(result.skipped[0]?.reason).toBe("forbidden");
  });

  it("sends one-element agent_ids for the single-agent case", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(VALID_MIGRATE_BODY));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.migrateAgentsToRuntime("runtime-b", { agent_ids: ["agent-1"] });

    const call = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(String(call[1].body)).agent_ids).toEqual(["agent-1"]);
  });

  it("keeps an unknown skip reason rather than dropping the entry", async () => {
    // The reason is a plain string in the schema on purpose: a reason added
    // server-side must still reach the UI, which switches on it with a
    // default branch, instead of vanishing from the skip list.
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        ...VALID_MIGRATE_BODY,
        skipped: [{ agent_id: "agent-9", reason: "some_future_reason" }],
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.migrateAgentsToRuntime("runtime-b", {
      agent_ids: ["agent-9"],
    });
    expect(result.skipped).toHaveLength(1);
    expect(result.skipped[0]?.reason).toBe("some_future_reason");
  });

  it("falls back to a zero-count report for a malformed success body", async () => {
    // The fallback must never claim work happened. Zero counts and empty lists
    // read as "we can't tell you what moved"; the caller still invalidates the
    // agent list, so the real state comes back from the server on the next read.
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ migrated: "not-an-array", tasks_migrated: "three" }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.migrateAgentsToRuntime("runtime-b", {
      agent_ids: ["agent-1"],
    });

    expect(result.migrated).toEqual([]);
    expect(result.skipped).toEqual([]);
    expect(result.tasks_migrated).toBe(0);
    expect(result.tasks_staying_active).toBe(0);
  });

  it("rejects on the stale-plan conflict instead of falling back", async () => {
    // 409 means nothing was written. Swallowing it into a zero-count success
    // would tell the user their migration ran when it did not.
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          error: "the agent set on this runtime changed",
          code: "runtime_migration_plan_changed",
          active_agents: [],
        },
        409,
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.migrateAgentsToRuntime("runtime-b", {
        agent_ids: ["agent-1"],
        expected_source_runtime_id: "runtime-a",
      }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("ApiClient.mergeAgentsEnv", () => {
  const VALID_MERGE_BODY = {
    results: [
      {
        agent_id: "agent-1",
        name: "Lambda",
        added_keys: ["NEW_KEY"],
        overwritten_keys: ["OLD_KEY"],
        key_count: 4,
      },
    ],
    skipped: [],
  };

  it("PATCHes the collection endpoint with the submitted keys", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(VALID_MERGE_BODY));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.mergeAgentsEnv({
      agent_ids: ["agent-1", "agent-2"],
      set: { NEW_KEY: "value" },
    });

    const call = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(call[0]).toContain("/api/agents/env");
    expect(call[1].method).toBe("PATCH");
    expect(JSON.parse(String(call[1].body))).toEqual({
      agent_ids: ["agent-1", "agent-2"],
      set: { NEW_KEY: "value" },
    });
    expect(result.results[0]?.added_keys).toEqual(["NEW_KEY"]);
    expect(result.results[0]?.key_count).toBe(4);
  });

  it("falls back to an empty report for a malformed success body", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ results: { agent_id: "agent-1" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.mergeAgentsEnv({
      agent_ids: ["agent-1"],
      set: { KEY: "value" },
    });

    expect(result.results).toEqual([]);
    expect(result.skipped).toEqual([]);
  });

  it("tolerates a result entry missing its optional key lists", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ results: [{ agent_id: "agent-1" }], skipped: [] }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const result = await client.mergeAgentsEnv({
      agent_ids: ["agent-1"],
      set: { KEY: "value" },
    });

    expect(result.results).toHaveLength(1);
    expect(result.results[0]?.added_keys).toEqual([]);
    expect(result.results[0]?.overwritten_keys).toEqual([]);
  });
});
