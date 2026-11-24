// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { agentRuntimePreferenceOptions, runtimeKeys } from "../runtimes/queries";

afterEach(() => vi.unstubAllGlobals());

describe("personal runtime preference API", () => {
  const client = new ApiClient("https://example.test");
  it.each(["runtime-1", null])("parses %s and preserves only personal preference", async (runtimeId) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ runtime_id: runtimeId, other_users: [] }))));
    expect(await client.getAgentRuntimePreference("agent-1", "ws-1")).toEqual({ runtimeId });
  });
  it.each([{}, null, [], { runtime_id: 7 }, { runtime_id: "" }, { runtime_id: null, provider: 42 }])("returns unavailable for malformed response %j", async (payload) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(payload))));
    expect(await client.getAgentRuntimePreference("agent-1", "ws-1")).toBeNull();
  });
  it("parses source provider without exposing default machine details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"runtime_id":null,"provider":"codex"}')));
    expect(await client.getAgentRuntimePreference("agent-1", "ws-1")).toEqual({ runtimeId: null, provider: "codex" });
  });
  it.each([{}, null, [], { runtime_id: 7 }, { runtime_id: "mine", max_concurrent_tasks: 0 }])("rejects malformed update response %j", async (payload) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(payload))));
    expect(await client.updateAgentRuntimePreference("agent-1", "ws-1", "mine")).toBeNull();
  });
  it("resets through the preference endpoint with explicit workspace", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{"runtime_id":null}'));
    vi.stubGlobal("fetch", fetch);
    expect(await client.updateAgentRuntimePreference("agent-1", "ws-1", null)).toEqual({ runtimeId: null });
    expect(fetch).toHaveBeenCalledWith(expect.stringContaining("/api/agents/agent-1/runtime-preference?workspace_id=ws-1"), expect.objectContaining({ headers: expect.objectContaining({ "X-Workspace-ID": "ws-1", "X-Workspace-Slug": "" }), method: "PUT", body: '{"runtime_id":null}' }));
  });
  it("scopes preference caches to workspace and agent", () => {
    expect(agentRuntimePreferenceOptions("ws-1", "agent-1").queryKey).toEqual(runtimeKeys.preference("ws-1", "agent-1"));
    expect(runtimeKeys.preference("ws-1", "agent-1")).not.toEqual(runtimeKeys.preference("ws-2", "agent-1"));
    expect(runtimeKeys.preference("ws-1", "agent-1")).not.toEqual(runtimeKeys.preference("ws-1", "agent-2"));
  });
});

it("round-trips personal model and concurrency with server field names", async () => {
 const client = new ApiClient("https://example.test");
 const raw = {runtime_id:"mine",model_mode:"custom",model:"custom-model",max_concurrent_tasks:3};
 const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(raw)));vi.stubGlobal("fetch",fetch);
 const preference = {runtimeId:"mine",modelMode:"custom" as const,model:"custom-model",maxConcurrentTasks:3};
 expect(await client.updateAgentRuntimePreference("agent","ws",preference)).toEqual(preference);
 expect(fetch).toHaveBeenCalledWith(expect.any(String),expect.objectContaining({body:JSON.stringify(raw)}));
});
it.each([{model_mode:"unknown"},{max_concurrent_tasks:0},{max_concurrent_tasks:1.5},{max_concurrent_tasks:101}])("rejects malformed execution settings %j", async (settings) => {
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({runtime_id:"mine",...settings}))));
 expect(await new ApiClient("https://example.test").getAgentRuntimePreference("agent","ws")).toBeNull();
});

describe("viewer runtime projection", () => {
  it.each(["online", "unstable", "offline"])("parses a personal %s projection independently from the shared runtime", async (availability) => {
    const agent = { id: "agent-1", runtime_id: "shared", personal_runtime_id: "mine", personal_runtime_availability: availability, runtime_availability: "offline" };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(agent))));
    expect(await new ApiClient("https://example.test").getAgent("agent-1")).toEqual(agent);
  });
  it("drops malformed projection fields without discarding the shared agent", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify([{ id: "agent-1", runtime_id: "shared", personal_runtime_id: [], personal_runtime_availability: "unknown" }]))));
    const [agent] = await new ApiClient("https://example.test").listAgents();
    expect(agent?.id).toBe("agent-1");
    expect(agent?.personal_runtime_id).toBeUndefined();
    expect(agent?.personal_runtime_availability).toBeUndefined();
  });
});
