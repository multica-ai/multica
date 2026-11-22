// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const api = new ApiClient("https://api.example.test");

function respond(body: unknown) {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

afterEach(() => vi.unstubAllGlobals());

describe("smart runtime response compatibility", () => {
  it.each([
    ["GET recent", () => api.getRecentAgentSelections("workspace")],
    ["POST recent", () => api.recordRecentAgentSelection("agent", "workspace")],
  ] as const)("returns an empty list for malformed %s responses", async (_, request) => {
    respond({ agent_ids: [42] });
    expect(await request()).toEqual({ agentIds: [] });
  });

  it("validates agent responses and treats a missing strategy as fixed", async () => {
    respond([{ id: "old", runtime_id: "fixed" }, { id: "smart", share_runtime: false, runtime_config_summary: { recommended: { cli: "codex" }, current: { runtime_id: "personal" } } }]);
    expect(await api.listAgents()).toMatchObject([
      { id: "old", runtime_id: "fixed" },
      { id: "smart", share_runtime: false, runtime_config_summary: { current: { runtime_id: "personal" } } },
    ]);
    respond({ id: false });
    await expect(api.getAgent("agent")).rejects.toThrow("Invalid agent response");
    respond({ id: false });
    await expect(api.createAgent({ name: "Agent", runtime_id: "", share_runtime: false, cli: "codex" })).rejects.toThrow("Invalid agent response");
    respond({ id: false });
    await expect(api.updateAgent("agent", { share_runtime: false })).rejects.toThrow("Invalid agent response");
  });

  it("deduplicates and caps recent IDs in server order", async () => {
    const ids = Array.from({ length: 12 }, (_, index) => String(index));
    const fetch = respond({ agent_ids: [ids[0], ...ids] });
    expect(await api.recordRecentAgentSelection("agent", "workspace")).toEqual({ agentIds: ids.slice(0, 10) });
    const request = new Headers(fetch.mock.calls[0]?.[1]?.headers);
    expect(request.get("X-Workspace-ID")).toBe("workspace");
    expect(fetch.mock.calls[0]?.[1]?.body).toBe(JSON.stringify({ agent_id: "agent" }));
  });

  it.each([
    ["GET current", () => api.getAgentRuntimeCurrentConfig("agent")],
    ["PUT current", () => api.updateAgentRuntimeCurrentConfig("agent", { strategy: "custom" })],
    ["POST notice-seen", () => api.markAgentRuntimeConfigNoticeSeen("agent")],
  ] as const)("does not report a usable personal runtime for malformed %s responses", async (_, request) => {
    respond({ recommended: {}, current: { runtime_id: true } });
    expect((await request()).current).toMatchObject({ runtime_id: null, editable: false, resolution_status: "runtime_unavailable" });
  });

  it("does not report migrated agents for a malformed preview", async () => {
    respond({ migrated: [{ agent_id: 12 }] });
    expect(await api.migrateAgentsToRuntime("runtime", {
      agentIds: ["agent"], dryRun: true, model: "", thinkingLevel: "", serviceTier: "", maxConcurrentTasks: 1,
    })).toMatchObject({ migrated: [], targetRuntimeId: "" });
  });

  it("drops a malformed optional quick-create hint and requires a task ID", async () => {
    respond({ task_id: "task", runtime_config_hint: { kind: "unexpected" } });
    expect(await api.quickCreateIssue({ prompt: "Create an issue" })).toMatchObject({ task_id: "task", runtime_config_hint: undefined });
    respond({ task_id: false });
    await expect(api.quickCreateIssue({ prompt: "Create an issue" })).rejects.toThrow("Invalid quick-create response");
  });

  it("retains task snapshots and drops malformed optional runtime fields", async () => {
    respond([{ id: "task", runtime_id: "runtime", runtime_owner_id: "owner", runtime_provider: "codex", effective_model: "model", runtime_config_hint: false }, { id: "old", runtime_owner_id: false }]);
    expect(await api.listTasksByIssue("issue")).toMatchObject([
      { id: "task", runtime_id: "runtime", runtime_owner_id: "owner", runtime_provider: "codex", effective_model: "model", runtime_config_hint: undefined },
      { id: "old", runtime_owner_id: undefined },
    ]);
  });
});
