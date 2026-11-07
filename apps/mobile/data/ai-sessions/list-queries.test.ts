// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { localV2SummarySchema, type LocalV2SessionPage } from "@multica/core/local-conversation/v2";
import { refreshAISessionBrowse, AISessionClient } from "./client";
import { sessionGroupsOptions, sessionPageCursors, sessionPageOptions, sessionPageSummaries } from "./list-queries";

const scope = { userId: "user", workspaceId: "workspace", deviceId: "device" };
const clients: AISessionClient[] = [], caches: QueryClient[] = [];
afterEach(() => { for (const client of clients.splice(0)) client.close(); for (const cache of caches.splice(0)) cache.clear(); });
const row = (id: string, seq = 1) => localV2SummarySchema.parse({ conversation_id: id, summary_seq: seq, provider: "codex", title: id, status: "idle", pinned: false, archived: false, updated_at: "2026-11-03" });

it("requests only one eight-row directory page and reads the next cursor only on demand", async () => {
  const first: LocalV2SessionPage = { scope_seq: 20, summaries: Array.from({ length: 8 }, (_, i) => row(`row-${i}`)), has_more: true, next_cursor: "next-page" };
  const last: LocalV2SessionPage = { scope_seq: 20, summaries: [row("row-8")], has_more: false, next_cursor: null };
  const fetcher = vi.fn<typeof fetch>(async (input) => {
    const url = new URL(String(input));
    if (url.pathname.endsWith("/session-groups")) return Response.json({ scope_seq: 20, groups: [{ id: "folder", title: "Folder", path: "/repo", total: 9 }] });
    if (!url.pathname.endsWith("/sessions/browse")) throw new Error("Full-directory endpoint requested");
    return Response.json(url.searchParams.has("cursor") ? last : first);
  });
  const qc = new QueryClient(); caches.push(qc);
  const client = new AISessionClient({ url: "http://127.0.0.1:1234", token: "native" }, scope, qc, () => { throw new Error("Unexpected socket"); }, fetcher); clients.push(client);
  await qc.fetchQuery(sessionGroupsOptions(client, false, ""));
  const initial = await qc.fetchQuery(sessionPageOptions(client, "folder", false, "", ""));
  expect(fetcher).toHaveBeenCalledTimes(2);
  const request = new URL(String(fetcher.mock.calls[1][0]));
  expect(request.searchParams.get("limit")).toBe("8");
  expect(request.searchParams.get("folder_id")).toBe("folder");
  expect(request.searchParams.has("cursor")).toBe(false);
  expect(initial.summaries).toHaveLength(8);
  const next = await qc.fetchQuery(sessionPageOptions(client, "folder", false, "", initial.next_cursor!));
  expect(fetcher).toHaveBeenCalledTimes(3);
  expect(new URL(String(fetcher.mock.calls[2][0])).searchParams.get("cursor")).toBe("next-page");
  expect(next.next_cursor).toBeNull();
  expect(qc.getQueryData(sessionPageOptions(client, "folder", false, "", "").queryKey)).toEqual(first);
  const key = sessionPageOptions(client, "folder", false, "", "").queryKey;
  for (const other of [sessionPageOptions(client, "other", false, "", ""), sessionPageOptions(client, "folder", true, "", ""), sessionPageOptions(client, "folder", false, "search", "")]) expect(other.queryKey).not.toEqual(key);
});

it("retains newer summary revisions when refreshed seek pages overlap", () => {
  const page = (summaries: ReturnType<typeof row>[]): LocalV2SessionPage => ({ scope_seq: 10, summaries, has_more: false, next_cursor: null });
  const newest = row("moving", 9);
  expect(sessionPageSummaries([page([newest, row("other")]), undefined, page([row("moving", 2)])])).toEqual([newest, row("other")]);
});

it("follows refreshed page boundaries up to the requested depth without reading unloaded pages", () => {
  const page = (next_cursor: string | null): LocalV2SessionPage => ({ scope_seq: 10, summaries: [], has_more: !!next_cursor, next_cursor });
  const pages = new Map([["", page("old-boundary")], ["old-boundary", page("old-third")]]);
  const read = vi.fn((cursor: string) => pages.get(cursor));
  expect(sessionPageCursors(1, read)).toEqual([""]);
  expect(read).not.toHaveBeenCalled();
  expect(sessionPageCursors(3, read)).toEqual(["", "old-boundary", "old-third"]);
  pages.set("", page("new-boundary"));
  expect(sessionPageCursors(3, read)).toEqual(["", "new-boundary"]);
  pages.set("new-boundary", page("new-third"));
  expect(sessionPageCursors(3, read)).toEqual(["", "new-boundary", "new-third"]);
  pages.set("new-boundary", page(null));
  expect(sessionPageCursors(3, read)).toEqual(["", "new-boundary"]);
  pages.set("new-boundary", page("new-boundary"));
  expect(sessionPageCursors(3, read)).toEqual(["", "new-boundary"]);
});

it("refreshes again when a completion arrives during the first group request", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); caches.push(qc);
  let resolve!: (value: unknown) => void;
  const old = { scope_seq: 1, groups: [{ id: "folder", title: "Folder", path: "/repo", total: 9, has_unread_completion: false }] };
  const latest = { ...old, scope_seq: 2, groups: [{ ...old.groups[0], has_unread_completion: true }] };
  const fetcher = vi.fn<typeof fetch>().mockImplementationOnce(async () => Response.json(await new Promise((done) => { resolve = done; }))).mockResolvedValue(Response.json(latest));
  const client = new AISessionClient({url: "http://localhost:1234", token: "test"}, scope, qc, () => { throw new Error("no socket"); }, fetcher); clients.push(client);
  const options = sessionGroupsOptions(client, false, "");
  const observer = new QueryObserver(qc, options);
  const stop = observer.subscribe(() => {});
  const refresh = refreshAISessionBrowse(qc, scope);
  resolve(old); await refresh;
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(qc.getQueryData(options.queryKey)).toEqual(latest);
  stop();
});
