import { useState } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createQueryClient } from "@multica/core/query-client";
import { issueKeys } from "@multica/core/issues/queries";
import { useRecentIssuesStore } from "@multica/core/issues/stores/recent-issues-store";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { SetParentIssueModal } from "./set-parent-issue";

const mocks = vi.hoisted(() => ({ wsId: "ws-a", success: vi.fn(), error: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => mocks.wsId }));
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(undefined),
}));
vi.mock("sonner", () => ({ toast: { success: mocks.success, error: mocks.error } }));

function issue(id: string, extra: Partial<Issue> = {}): Issue {
  return {
    id, workspace_id: "ws-a", identifier: `TEST-${id}`, title: `Fresh ${id}`,
    description: null, assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "synthetic",
    parent_issue_id: null, project_id: null, position: 0, start_date: null, due_date: null, metadata: {}, properties: {},
    number: 1, status: "todo", priority: "none", revision: 1,
    created_at: "2026-11-05T00:00:00Z", updated_at: "2026-11-05T00:00:00Z", ...extra,
  } as Issue;
}
function recent(ids: string[], wsId = "ws-a") {
  useRecentIssuesStore.setState((s) => ({ byWorkspace: {
    ...s.byWorkspace, [wsId]: ids.map((id, i) => ({ id, visitedAt: 100 - i })),
  } }));
}
function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
let client: ReturnType<typeof createQueryClient>;
let requests: { id: string; signal?: AbortSignal | null }[];
let detail: (id: string) => Response | Promise<Response>;
let searchResults: Issue[];
let mutationStatus: number;
let mutationBodies: Record<string, unknown>[];
const fetchMock = vi.fn();
function mount(onClose = vi.fn()) {
  return renderWithI18n(<QueryClientProvider client={client}>
    <SetParentIssueModal data={{ issueId: "self" }} onClose={onClose} />
  </QueryClientProvider>);
}
beforeEach(() => {
  mocks.wsId = "ws-a";
  vi.clearAllMocks();
  requests = []; mutationBodies = []; mutationStatus = 200; searchResults = [];
  client = createQueryClient();
  useRecentIssuesStore.setState({ byWorkspace: {} });
  detail = (id) => response(issue(id));
  setApiInstance(new ApiClient("http://synthetic.local"));
  fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
    const path = new URL(url).pathname;
    if (path.endsWith("/children")) return response({ issues: [issue("child")] });
    if (path.endsWith("/search")) return response({ issues: searchResults.map((i) => ({ ...i, match_source: "title" })), total: searchResults.length });
    const id = path.split("/").at(-1)!;
    if (init?.method === "PUT") {
      mutationBodies.push(JSON.parse(init.body as string));
      return response(mutationStatus === 200 ? issue(id) : { error: "Circular parent rejected" }, mutationStatus);
    }
    requests.push({ id, signal: init?.signal });
    return detail(id);
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); });

describe("parent picker recently viewed suggestions (synthetic API transport)", () => {
  it("shows current-workspace recent choices on empty query in store order, excludes self and direct children, and selects through the existing mutation", async () => {
    recent(["self", "child", "newest", "older", "newest"]);
    recent(["other-workspace"], "ws-b");
    const closed = vi.fn();
    mount(closed);
    await screen.findByText("Fresh newest");
    expect(screen.getByText("Recently viewed")).toBeVisible();
    expect(screen.getAllByRole("option").map((x) => x.textContent)).toEqual([
      expect.stringContaining("Fresh newest"), expect.stringContaining("Fresh older"),
    ]);
    expect(screen.queryByText("Fresh self")).toBeNull();
    expect(screen.queryByText("Fresh child")).toBeNull();
    expect(requests.some((x) => x.id === "other-workspace")).toBe(false);
    fireEvent.click(screen.getByText("Fresh newest"));
    await waitFor(() => expect(mutationBodies).toEqual([expect.objectContaining({ parent_issue_id: "newest" })]));
    await waitFor(() => expect(mocks.success).toHaveBeenCalledOnce());
    expect(closed).toHaveBeenCalledOnce();
  });

  it("still reports a server rejection after selection closes and unmounts the parent modal", async () => {
    recent(["deeper"]);
    mutationStatus = 400;
    function ClosingPicker() {
      const [open, setOpen] = useState(true);
      return open ? <SetParentIssueModal data={{ issueId: "self" }} onClose={() => setOpen(false)} /> : null;
    }
    renderWithI18n(<QueryClientProvider client={client}><ClosingPicker /></QueryClientProvider>);
    await screen.findByText("Fresh deeper");
    fireEvent.click(screen.getByText("Fresh deeper"));
    await waitFor(() => expect(mocks.error).toHaveBeenCalledWith("Circular parent rejected"));
    expect(screen.queryByText("Fresh deeper")).toBeNull();
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("has no suggested rows without history and does not fetch arbitrary workspace issues", async () => {
    mount();
    expect(screen.queryByText("Recently viewed")).toBeNull();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(requests).toHaveLength(0);
  });

  it.each([403, 404, 500, "network", "abort"])("never exposes fresh cached titles before this open succeeds or after %s; other candidates still load without retries", async (failure) => {
    recent(["denied", "allowed"]);
    client.setQueryData(issueKeys.detail("ws-a", "denied"), issue("denied", { title: "Cached secret" }));
    let reject!: (error: Error) => void;
    let resolve!: (value: Response) => void;
    detail = (id) => id === "denied" ? new Promise<Response>((yes, no) => { resolve = yes; reject = no; }) : response(issue(id));
    mount();
    await screen.findByText("Fresh allowed");
    expect(screen.queryByText("Cached secret")).toBeNull();
    await act(async () => {
      if (typeof failure === "number") resolve(response({ error: "denied" }, failure));
      else reject(failure === "abort" ? new DOMException("Aborted", "AbortError") : new Error("Offline"));
    });
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(screen.queryByText("Cached secret")).toBeNull();
    expect(screen.queryByText("Fresh denied")).toBeNull();
    expect(screen.getByText("Fresh allowed")).toBeVisible();
    expect(requests.filter((x) => x.id === "denied")).toHaveLength(1);
    expect(client.getQueryData<Issue>(issueKeys.detail("ws-a", "denied"))?.title).toBe("Cached secret");
  });

  it("bounds and deduplicates history to 20, regardless of response order", async () => {
    recent(Array.from({ length: 25 }, (_, i) => `recent-${i}`));
    const resolves: (() => void)[] = [];
    detail = (id) => new Promise<Response>((resolve) => { resolves.push(() => resolve(response(issue(id)))); });
    mount();
    await waitFor(() => expect(requests).toHaveLength(20));
    await act(async () => { resolves.reverse().forEach((resolve) => resolve()); });
    await screen.findByText("Fresh recent-19");
    expect(screen.getAllByRole("option").map((row) => row.textContent)).toEqual(
      Array.from({ length: 20 }, (_, i) => expect.stringContaining(`Fresh recent-${i}`)),
    );
    expect(requests).toHaveLength(20);
    expect(screen.queryByText("Fresh recent-20")).toBeNull();
  });

  it("drops mismatched workspace entities even after a successful response", async () => {
    recent(["mismatch", "allowed"]);
    detail = (id) => response(issue(id, id === "mismatch" ? { workspace_id: "ws-b" } : {}));
    mount();
    await screen.findByText("Fresh allowed");
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(screen.queryByText("Fresh mismatch")).toBeNull();
  });

  it("aborts old workspace requests and ignores their late responses", async () => {
    recent(["late-a"]); recent(["current-b"], "ws-b");
    let resolve!: (value: Response) => void;
    detail = (id) => id === "late-a" ? new Promise<Response>((yes) => { resolve = yes; }) : response(issue(id, { workspace_id: "ws-b" }));
    const view = mount();
    await waitFor(() => expect(requests).toHaveLength(1));
    mocks.wsId = "ws-b";
    view.rerender(<QueryClientProvider client={client}><SetParentIssueModal data={{ issueId: "self" }} onClose={vi.fn()} /></QueryClientProvider>);
    await screen.findByText("Fresh current-b");
    expect(requests[0]!.signal?.aborted).toBe(true);
    await act(async () => resolve(response(issue("late-a"))));
    expect(screen.queryByText("Fresh late-a")).toBeNull();
  });

  it("remounts with a new authorization read; a late closed-session response cannot refill the reopened picker", async () => {
    recent(["entry"]);
    let resolve!: (value: Response) => void;
    detail = () => new Promise<Response>((yes) => { resolve = yes; });
    const first = mount();
    await waitFor(() => expect(requests).toHaveLength(1));
    first.unmount();
    expect(requests[0]!.signal?.aborted).toBe(true);
    detail = () => response({ error: "denied on reopen" }, 403);
    mount();
    await waitFor(() => expect(requests).toHaveLength(2));
    await act(async () => resolve(response(issue("entry", { title: "Late secret" }))));
    await waitFor(() => expect(client.isFetching()).toBe(0));
    expect(screen.queryByText("Late secret")).toBeNull();
    expect(screen.queryByText("Recently viewed")).toBeNull();
  });

  it("preserves explicit search including done/cancelled, restores recents on clear, and surfaces server rejection of a deeper relation without success", async () => {
    recent(["deeper"]);
    searchResults = [issue("self"), issue("child"), issue("done", { status: "done" }), issue("cancelled", { status: "cancelled" })];
    mount();
    await screen.findByText("Fresh deeper");
    const input = screen.getByPlaceholderText("Search issues...");
    fireEvent.change(input, { target: { value: "closed" } });
    await screen.findByText("Fresh done");
    expect(screen.getByText("Fresh cancelled")).toBeVisible();
    expect(screen.queryByText("Fresh self")).toBeNull();
    expect(screen.queryByText("Fresh child")).toBeNull();
    expect(fetchMock.mock.calls.some(([url]) => new URL(url).searchParams.get("include_closed") === "true")).toBe(true);
    fireEvent.change(input, { target: { value: "" } });
    await screen.findByText("Fresh deeper");
    mutationStatus = 400;
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(mocks.error).toHaveBeenCalledWith("Circular parent rejected"));
    expect(mocks.success).not.toHaveBeenCalled();
    expect(mutationBodies).toEqual([expect.objectContaining({ parent_issue_id: "deeper" })]);
  });
});
