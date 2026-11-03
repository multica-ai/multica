// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { AgentSchema, EMPTY_ISSUE_FALLBACK } from "@/data/schemas";
import type { AISessionClient } from "./client";
import { loadReferencePage, referencePageOptions } from "./reference-queries";

const api = vi.hoisted(() => ({ listAgents: vi.fn(), listMembers: vi.fn(), listIssues: vi.fn(), searchIssues: vi.fn() }));
vi.mock("@/data/api", () => ({ api }));
const scope = { userId: "user", workspaceId: "workspace", deviceId: "device" };
const queries: QueryClient[] = [];
const queryClient = () => { const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); queries.push(qc); return qc; };
const signal = () => new AbortController().signal;
afterEach(() => { for (const qc of queries.splice(0)) qc.clear(); vi.clearAllMocks(); });

describe("AI reference pagination", () => {
  it("starts each category at eight and keeps independent cached pages", async () => {
    const qc = queryClient();
    const sessions = Array.from({ length: 17 }, (_, index) => localV2SummarySchema.parse({ conversation_id: `s${index}`, summary_seq: index, provider: "codex", title: `Session ${index}`, status: "idle", pinned: false, archived: false, updated_at: `2026-11-${String(index + 1).padStart(2, "0")}` }));
    const client = { scope, sessionPage: vi.fn(async ({ cursor }: { cursor?: string }) => {
      const offset = cursor ? Number(cursor.split("-")[1]) : 0;
      const sorted = [...sessions].reverse();
      return { summaries: sorted.slice(offset, offset + 8), scope_seq: 17, has_more: offset + 8 < sorted.length, next_cursor: offset + 8 < sorted.length ? `cursor-${offset + 8}` : null };
    }) } as unknown as AISessionClient;
    api.listAgents.mockResolvedValue(Array.from({ length: 17 }, (_, index) => AgentSchema.parse({ id: `a${index}`, workspace_id: scope.workspaceId, name: `Agent ${index}`, runtime_id: "runtime", owner_id: scope.userId })));
    api.listMembers.mockResolvedValue([{ user_id: scope.userId, role: "member" }]);
    const taskRows = Array.from({ length: 9 }, (_, index) => ({ ...EMPTY_ISSUE_FALLBACK, id: `i${index}`, workspace_id: scope.workspaceId }));
    api.listIssues.mockResolvedValue({ issues: taskRows, total: 17 });
    for (const kind of ["session", "issue", "agent"] as const) {
      const data = await qc.fetchInfiniteQuery(referencePageOptions(client, qc, "current", kind, ""));
      expect(data.pages[0].items).toHaveLength(8);
      expect(data.pages[0].nextOffset).toBe(kind === "session" ? "cursor-8" : 8);
    }
    const more = await loadReferencePage(client, qc, "current", "session", "", "cursor-8", signal());
    expect(more.items).toHaveLength(8);
    expect(more.items[0].id).toBe("s8");
    const agentOptions = referencePageOptions(client, qc, "current", "agent", "");
    expect(qc.getQueryData<{ pages: unknown[] }>(agentOptions.queryKey)?.pages).toHaveLength(1);
    expect(client.sessionPage).toHaveBeenCalledTimes(2);
    expect(client.sessionPage).toHaveBeenNthCalledWith(1, expect.objectContaining({ excludeId: "current", limit: 8 }), expect.anything());
    expect(api.listIssues).toHaveBeenCalledWith(expect.objectContaining({ limit: 9, offset: 0, workspace_id: scope.workspaceId }), expect.anything());
    expect(await loadReferencePage(client, qc, "current", "session", "", "cursor-16", signal())).toMatchObject({ items: [expect.objectContaining({ id: "s0" })], nextOffset: undefined });
  });
  it("searches all tasks on the server, forwards cancellation and retains results on next-page failure", async () => {
    const qc = queryClient(), client = { scope } as AISessionClient;
    const controller = new AbortController();
    const tasks = Array.from({ length: 9 }, (_, index) => ({ ...EMPTY_ISSUE_FALLBACK, id: `i${index}`, workspace_id: scope.workspaceId }));
    api.searchIssues.mockResolvedValueOnce({ issues: tasks }).mockRejectedValueOnce(new Error("offline"));
    const options = referencePageOptions(client, qc, "current", "issue", "keyboard");
    const initial = await qc.fetchInfiniteQuery(options);
    await expect(loadReferencePage(client, qc, "current", "issue", "keyboard", 8, controller.signal)).rejects.toThrow("offline");
    expect(qc.getQueryData(options.queryKey)).toEqual(initial);
    expect(api.searchIssues).toHaveBeenLastCalledWith({ q: "keyboard", limit: 9, offset: 8, include_closed: true }, { signal: controller.signal });
  });
  it("delegates session exclusions to the bounded server page and keeps agent invocation restrictions", async () => {
    const qc = queryClient();
    const client = { scope, sessionPage: vi.fn(async () => ({ summaries: [], scope_seq: 2, has_more: false, next_cursor: null })) } as unknown as AISessionClient;
    expect((await loadReferencePage(client, qc, "current", "session", "", 0, signal())).items).toEqual([]);
    expect(client.sessionPage).toHaveBeenCalledWith(expect.objectContaining({ excludeId: "current", limit: 8 }), expect.anything());
    api.listMembers.mockResolvedValue([{ user_id: scope.userId, role: "owner" }]);
    const privateAgent = AgentSchema.parse({ id: "private", name: "Private agent", workspace_id: scope.workspaceId, runtime_id: "runtime", owner_id: "other", permission_mode: "private" });
    api.listAgents.mockResolvedValue([privateAgent, { ...privateAgent, id: "archived", archived_at: "now" }, { ...privateAgent, id: "unbound", runtime_id: "", owner_id: scope.userId }]);
    const page = await loadReferencePage(client, qc, "current", "agent", "", 0, signal());
    expect(page.items).toHaveLength(2);
    expect(page.items.map((item) => item.unavailable).sort()).toEqual(["needs_runtime", "no_permission"]);
    expect(page.nextOffset).toBeUndefined();
  });
});
