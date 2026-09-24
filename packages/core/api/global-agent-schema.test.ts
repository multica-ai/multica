// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { parseWithFallback } from "./schema";
import {
  EMPTY_GLOBAL_AGENT,
  EMPTY_GLOBAL_AGENT_LIST,
  EMPTY_GLOBAL_AGENT_WORKSPACE_TARGETS,
  GlobalAgentListSchema,
  GlobalAgentSchema,
  GlobalAgentWorkspaceTargetListSchema,
} from "./schemas";

const baseGlobalAgent = {
  id: "ga-1",
  owner_id: "user-1",
  name: "Reviewer",
  description: "Reviews pull requests",
  instructions: "Be thorough.",
  avatar_url: "emoji:🦉",
  conversation_starters: [{ label: "Review", prompt: "Review the open PR." }],
  links: [
    {
      workspace_id: "ws-1",
      workspace_name: "Acme",
      workspace_slug: "acme",
      agent_id: "agent-1",
      archived: false,
      runtime_bound: true,
    },
  ],
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
};

const baseTarget = {
  workspace_id: "ws-1",
  workspace_name: "Acme",
  workspace_slug: "acme",
  agent: { id: "agent-1", archived: false, runtime_id: "rt-1" },
  runtimes: [
    { id: "rt-1", name: "MacBook (Claude)", provider: "claude", status: "online", owned_by_me: true },
  ],
  suggested_runtime_id: "rt-1",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("GlobalAgentSchema", () => {
  it("parses a complete global agent", () => {
    const parsed = GlobalAgentSchema.parse(baseGlobalAgent);
    expect(parsed.links).toHaveLength(1);
    expect(parsed.links[0]?.workspace_slug).toBe("acme");
    expect(parsed.conversation_starters[0]?.label).toBe("Review");
  });

  it("degrades drifted fields without dropping the agent", () => {
    const parsed = GlobalAgentSchema.parse({
      id: "ga-1",
      name: "Reviewer",
      description: null,
      avatar_url: undefined,
      conversation_starters: null,
      links: [{ workspace_id: "ws-1", agent_id: "agent-1", archived: "yes" }],
    });
    expect(parsed.description).toBe("");
    expect(parsed.avatar_url).toBeNull();
    expect(parsed.conversation_starters).toEqual([]);
    expect(parsed.links[0]).toMatchObject({
      workspace_name: "",
      archived: false,
      runtime_bound: true,
    });
  });

  it("falls back when an entry lacks its identity", () => {
    const parsed = parseWithFallback(
      [{ ...baseGlobalAgent, id: 42 }],
      GlobalAgentListSchema,
      EMPTY_GLOBAL_AGENT_LIST,
      { endpoint: "GET /api/global-agents" },
    );
    expect(parsed).toBe(EMPTY_GLOBAL_AGENT_LIST);
  });
});

describe("GlobalAgentWorkspaceTargetListSchema", () => {
  it("parses a workspace that has never had the agent", () => {
    const parsed = GlobalAgentWorkspaceTargetListSchema.parse([
      { ...baseTarget, agent: null, runtimes: [], suggested_runtime_id: "" },
    ]);
    expect(parsed[0]?.agent).toBeNull();
    expect(parsed[0]?.runtimes).toEqual([]);
  });

  it("defaults missing runtime and suggestion fields", () => {
    const parsed = GlobalAgentWorkspaceTargetListSchema.parse([
      {
        workspace_id: "ws-1",
        agent: "garbage",
        runtimes: [{ id: "rt-1" }],
      },
    ]);
    expect(parsed[0]?.agent).toBeNull();
    expect(parsed[0]?.suggested_runtime_id).toBe("");
    expect(parsed[0]?.runtimes[0]).toMatchObject({
      name: "",
      provider: "",
      owned_by_me: false,
    });
  });
});

describe("ApiClient global agents", () => {
  it("returns the list fallback for a malformed list response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ agents: [] })));
    const client = new ApiClient("https://api.example.test");
    await expect(client.listGlobalAgents()).resolves.toBe(EMPTY_GLOBAL_AGENT_LIST);
  });

  it("returns the empty agent for a malformed single response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ name: 1 })));
    const client = new ApiClient("https://api.example.test");
    await expect(client.getGlobalAgent("ga-1")).resolves.toBe(EMPTY_GLOBAL_AGENT);
  });

  it("returns the target fallback for a malformed workspaces response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(null)));
    const client = new ApiClient("https://api.example.test");
    await expect(client.listGlobalAgentWorkspaces("ga-1")).resolves.toBe(
      EMPTY_GLOBAL_AGENT_WORKSPACE_TARGETS,
    );
  });

  it("posts the enable body and yields null for an unrecognised agent payload", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ ok: true }, 201));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const result = await client.enableGlobalAgentInWorkspace("ga-1", {
      workspace_id: "ws-2",
      runtime_id: "rt-2",
    });
    expect(result).toBeNull();
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(String(url)).toBe("https://api.example.test/api/global-agents/ga-1/workspaces");
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({
      workspace_id: "ws-2",
      runtime_id: "rt-2",
    });
  });

  it("returns the linked agent when disabling in a workspace", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ id: "agent-2", workspace_id: "ws-2", name: "Reviewer" }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const result = await client.disableGlobalAgentInWorkspace("ga-1", "ws-2");
    expect(result).toMatchObject({ id: "agent-2", name: "Reviewer" });
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(String(url)).toBe("https://api.example.test/api/global-agents/ga-1/workspaces/ws-2");
    expect(init?.method).toBe("DELETE");
  });
});
