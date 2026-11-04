// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { AgentGuiHostEvent } from "@multica/tutti-agent-gui-host/protocol";
import type { Agent, StorageAdapter } from "@multica/core/types";
import { createAgentGuiWorkspaceService } from "./agent-gui-parent-service";

const mocked = vi.hoisted(() => ({ api: {
  listAgents: vi.fn(), listMembers: vi.fn(), listRuntimes: vi.fn(),
  listIssues: vi.fn(), listIssueStatuses: vi.fn(),
} }));
vi.mock("@multica/core/api", async (importOriginal) => ({ ...await importOriginal<typeof import("@multica/core/api")>(), api: mocked.api }));

const sessionId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const agent = { id: "agent", workspace_id: "ws", runtime_id: "runtime", runtime_bound: true,
  runtime_config: {}, runtime_availability: "online", visibility: "workspace", archived_at: null,
  name: "Code agent", owner_id: "user", permission_mode: "private", invocation_targets: [] } as unknown as Agent;
const cleanups: Array<() => void> = [];

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  let active = true;
  const values = new Map<string, string>();
  const storage: StorageAdapter = { getItem: (key) => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: (key) => { values.delete(key); } };
  const onOpenInternalResource = vi.fn();
  const onShowMessage = vi.fn();
  const service = createAgentGuiWorkspaceService({ queryClient: qc, workspaceId: "ws", workspaceSlug: "acme", userId: "user",
    storage, isCurrent: () => active, resolveAvatar: (url) => url,
    onOpenTask: vi.fn(), onOpenInternalResource, getAppOrigin: () => "https://app.example.test", onShowMessage });
  cleanups.push(() => { service.dispose(); qc.clear(); });
  return { service, qc, values, onOpenInternalResource, onShowMessage, leave: () => { active = false; } };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocked.api.listAgents.mockResolvedValue([agent]);
  mocked.api.listMembers.mockResolvedValue([{ user_id: "user", role: "owner" }]);
  mocked.api.listRuntimes.mockResolvedValue([{ id: "runtime", provider: "codex" }]);
  mocked.api.listIssueStatuses.mockResolvedValue({ statuses: [] });
});
afterEach(() => { cleanups.splice(0).forEach((fn) => fn()); });

describe("Agent GUI workspace service", () => {
  it.each([
    ["success", "success"], ["info", "info"], ["warning", "warning"], ["error", "error"],
    [undefined, "info"], ["unknown", "info"],
  ])("forwards message tone %s as %s", async (tone, expected) => {
    const { service, onShowMessage } = setup();
    await service.request("host.showMessage", { message: "Clipboard feedback", tone } as never);
    expect(onShowMessage).toHaveBeenCalledExactlyOnceWith("Clipboard feedback", expected);
  });
  it("opens validated internal resources without creating a conversation or calling a model", async () => {
    const { service, onOpenInternalResource } = setup();
    await service.request("host.openInternalResource", { href: `https://app.example.test/acme/agents/${sessionId}?view=general#avatar` });
    expect(onOpenInternalResource).toHaveBeenLastCalledWith(`/acme/agents/${sessionId}?view=general#avatar`, false);
    await service.request("host.openInternalResource", { href: "mention://issue/TES-124?workspaceId=ws", background: true });
    expect(onOpenInternalResource).toHaveBeenLastCalledWith("/acme/issues/TES-124", true);
  });
  it("rejects foreign or malformed internal navigation before opening anything", async () => {
    const { service, onOpenInternalResource, leave } = setup();
    for (const href of [`https://other.example/acme/agents/${sessionId}`, `https://app.example.test/other/agents/${sessionId}`, `mention://agent/${sessionId}?workspaceId=other`, "/acme/agents/new"]) {
      await expect(service.request("host.openInternalResource", { href })).rejects.toMatchObject({ code: "invalid_request" });
    }
    await expect(service.request("host.openInternalResource", { href: `/acme/agents/${sessionId}`, background: "true" } as never)).rejects.toMatchObject({ code: "invalid_request" });
    leave();
    await expect(service.request("host.openInternalResource", { href: `/acme/agents/${sessionId}` })).rejects.toBeInstanceOf(Error);
    expect(onOpenInternalResource).not.toHaveBeenCalled();
  });
  it("resolves custom task status names from the workspace catalog, including archived definitions", async () => {
    const { service } = setup();
    mocked.api.listIssues.mockResolvedValue({ issues: [{ id: "qa-task", workspace_id: "ws", identifier: "TES-1", title: "Review", status: "qa" }] });
    mocked.api.listIssueStatuses.mockResolvedValue({ statuses: [{ key: "qa", name: "等待验收", category: "started", is_system: false, archived_at: "2026-01-01" }] });
    expect(await service.request("context.tasks", { query: "", limit: 10 })).toEqual([expect.objectContaining({ status: "qa", statusLabel: "等待验收", statusCategory: "started" })]);
    expect(mocked.api.listIssueStatuses).toHaveBeenCalledWith(true);
  });
  it("keeps task references usable when an older server cannot provide its status catalog", async () => {
    const { service } = setup();
    mocked.api.listIssues.mockResolvedValue({ issues: [{ id: "task", workspace_id: "ws", identifier: "TES-1", title: "Review", status: "done" }] });
    mocked.api.listIssueStatuses.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
    expect(await service.request("context.tasks", { query: "", limit: 10 })).toEqual([expect.objectContaining({ id: "task", status: "done" })]);
  });
  it("preserves distinct Multica emoji and uploaded avatars through the GUI image-only port", async () => {
    mocked.api.listAgents.mockResolvedValue([
      { ...agent, id: "design", avatar_url: "emoji:🦉" },
      { ...agent, id: "code", avatar_url: "emoji:🐙" },
      { ...agent, id: "photo", avatar_url: "https://example.test/agent.png" },
      { ...agent, id: "default", avatar_url: null },
    ]);
    const { service } = setup();
    const { agents } = await service.load();
    expect(decodeURIComponent(agents[0]!.iconUrl)).toContain("🦉");
    expect(decodeURIComponent(agents[1]!.iconUrl)).toContain("🐙");
    expect(agents[0]!.iconUrl).not.toBe(agents[1]!.iconUrl);
    expect(agents[0]!.iconUrl).toMatch(/^data:image\/svg\+xml,/);
    expect(agents[2]!.iconUrl).toBe("https://example.test/agent.png");
    expect(decodeURIComponent(agents[3]!.iconUrl)).toContain("🤖");
  });
  it("uses runtime liveness when the agent availability summary is absent or stale", async () => {
    mocked.api.listAgents.mockResolvedValue([{ ...agent, runtime_availability: undefined }]);
    mocked.api.listRuntimes.mockResolvedValue([{ id: "runtime", provider: "codex", status: "online", last_seen_at: new Date().toISOString() }]);
    const { service } = setup();
    expect((await service.load()).agents[0]).toMatchObject({ runtimeOnline: true });
    mocked.api.listAgents.mockResolvedValue([agent]);
    mocked.api.listRuntimes.mockResolvedValue([{ id: "runtime", provider: "codex", status: "offline", last_seen_at: "2020-01-01T00:00:00Z" }]);
    expect((await service.load()).agents[0]).toMatchObject({ runtimeOnline: false });
  });

  it("uses real agent identities and scopes GUI persistence to the account and workspace", async () => {
    const { service, values } = setup();
    const state = await service.load();
    expect(state.agents[0]).toMatchObject({ agent, provider: "codex", canInvoke: true, runtimeBound: true });
    await service.request("storage.write", { key: "../../token", value: "session-aliases" });
    expect([...values.keys()]).toEqual(["multica:tutti-gui:user:ws:..%2F..%2Ftoken"]);
    expect(await service.request("storage.read", { key: "../../token" })).toBe("session-aliases");
  });

  it("queries tasks in the current workspace and rejects an invalid search", async () => {
    const { service } = setup();
    const issue = { id: "issue-1", workspace_id: "ws", identifier: "DEV-1", title: "Review", description: `Key sk-${"A".repeat(30)}`, status: "todo" };
    mocked.api.listIssues.mockResolvedValue({ issues: [issue, { ...issue, id: "foreign", workspace_id: "other" }] });
    const found = await service.request("context.tasks", { query: "Review", limit: 10 });
    expect(found).toEqual([expect.objectContaining({ id: "issue-1", label: "DEV-1 Review", description: "Key [REDACTED API KEY]" })]);
    expect(mocked.api.listIssues).toHaveBeenCalledWith(expect.objectContaining({ workspace_id: "ws", q: "Review", limit: 10 }));
    await expect(service.request("context.tasks", { query: "", limit: 0 })).rejects.toMatchObject({ code: "invalid_request" });
  });

  it("invalidates the workspace when its agents or runtimes change, and not for chat activity", async () => {
    const { service, qc } = setup();
    const events: AgentGuiHostEvent[] = [];
    service.subscribe((event) => events.push(event));
    await qc.invalidateQueries({ queryKey: ["chat", "ws", "sessions"] });
    await Promise.resolve();
    expect(events).toEqual([]);
    qc.setQueryData(["workspaces", "ws", "agents"], []);
    await qc.invalidateQueries({ queryKey: ["workspaces", "ws", "agents"] });
    await vi.waitFor(() => expect(events).toContainEqual({ type: "invalidate", scope: "workspace" }));
  });

  it("refuses conversation commands, which local conversations answer themselves", async () => {
    const { service } = setup();
    await expect(service.request("session.load", { sessionId })).rejects.toMatchObject({ code: "unsupported" });
  });

  it("rejects a load that completes after the workspace changed", async () => {
    const { service, leave } = setup();
    let resolve!: (value: Agent[]) => void;
    mocked.api.listAgents.mockReturnValue(new Promise<Agent[]>((done) => { resolve = done; }));
    const load = service.load();
    leave(); resolve([agent]);
    await expect(load).rejects.toMatchObject({ code: "stale_workspace" });
  });
});
