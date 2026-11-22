import { createServer, type Server } from "node:http";
import { useContext, type ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { setCurrentWorkspace } from "@multica/core/platform";
import type { Issue, IssueTableQuerySpec, UpdateIssueRequest } from "@multica/core/types";
import { toast } from "sonner";
import { NavigationProvider, useNavigation, type NavigationAdapter } from "../../navigation";
import { getTestResources } from "../../test/i18n";
import { ReviewDetailContext, ReviewSessionContext } from "./session";
import { useReviewCompletion } from "./use-review-completion";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(() => "completion-toast"), error: vi.fn(), dismiss: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "member" } }),
    { getState: () => ({ user: { id: "member" } }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/test/issues/${id}` }),
}));

const query: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: { statuses: ["in_review"], priorities: ["low"], negate: ["priorities"], include_sub_issues: true },
  sort: { field: "title", direction: "desc" },
};
function issue(index: number): Issue {
  return {
    id: `issue-${index}`, workspace_id: "ws", number: index + 1, identifier: `TEST-${index + 1}`,
    title: `Review ${index}`, description: null, status: "in_review", priority: "medium",
    assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "member",
    parent_issue_id: null, project_id: null, position: index, stage: null,
    start_date: null, due_date: null, metadata: {}, properties: {},
    created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", revision: 7,
  };
}

// A synthetic HTTP contract fixture, not the Go server or a SQL/CAS test.
// The client, schemas, queue traversal, mutation, hook and navigation provider
// are real. No mock replaces their collaboration across a route transition.
let server: Server;
let baseUrl: string;
let issues: Issue[];
let requests: { path: string; body: Record<string, unknown> }[];
let writes: Partial<UpdateIssueRequest>[];
let rejectWrite: { status: number; code: string; message: string } | undefined;
let heldWrite: Promise<void> | undefined;
let releaseWrite: (() => void) | undefined;
const clients: QueryClient[] = [];

beforeEach(async () => {
  vi.clearAllMocks();
  issues = Array.from({ length: 52 }, (_, index) => issue(index));
  requests = [];
  writes = [];
  rejectWrite = undefined;
  heldWrite = undefined;
  releaseWrite = undefined;
  setCurrentWorkspace("test", "ws");
  server = createServer(async (req, res) => {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(Buffer.from(chunk));
    const body = JSON.parse(Buffer.concat(chunks).toString() || "{}") as Record<string, unknown>;
    const path = req.url!;
    requests.push({ path, body });
    res.setHeader("Content-Type", "application/json");
    res.setHeader("Connection", "close");
    const send = (value: unknown, status = 200) => { res.statusCode = status; res.end(JSON.stringify(value)); };
    const live = issues.filter((item) => item.status === "in_review");
    if (req.method === "POST" && path === "/api/issues/table/groups") {
      send({ query_fingerprint: "source-query", total: live.length, next_cursor: null,
        groups: [{ key: "status:in_review", value: { kind: "status", status: "in_review" }, count: live.length }] });
    } else if (req.method === "POST" && path === "/api/issues/table/rows") {
      const cursor = (body.page as { cursor: string | null }).cursor;
      const offset = cursor === null ? 0 : Number(cursor);
      const page = live.slice(offset, offset + 50);
      send({ query_fingerprint: "source-query", group_key: "status:in_review", parent_id: null,
        total: live.length, branch_total: page.length,
        rows: page.map((item) => ({ issue: item, direct_child_count: 0 })),
        next_cursor: offset + 50 < live.length ? String(offset + 50) : null });
    } else if (req.method === "PUT" && path.startsWith("/api/issues/issue-")) {
      writes.push(body);
      const target = issues.find((item) => path === `/api/issues/${item.id}`)!;
      if (rejectWrite) {
        send({ code: rejectWrite.code, message: rejectWrite.message }, rejectWrite.status);
      } else if (target.revision !== body.expected_revision) {
        send({ code: "revision_conflict", message: "Concurrent edit" }, 409);
      } else {
        const saved = { ...target, status: body.status as string, revision: target.revision! + 1 };
        issues[issues.indexOf(target)] = saved;
        if (heldWrite) await heldWrite;
        send(saved);
      }
    } else {
      send({ message: `Unexpected fixture request: ${req.method} ${path}` }, 500);
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Missing loopback address");
  baseUrl = `http://127.0.0.1:${address.port}`;
  setApiInstance(new ApiClient(baseUrl));
});

afterEach(async () => {
  releaseWrite?.();
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
  setCurrentWorkspace(null, null);
});

function setup(current = issues[49]!, withOrigin = true) {
  const qc = new QueryClient({ defaultOptions: {
    queries: { retry: false }, mutations: { retry: false },
  } });
  clients.push(qc);
  const adapter: NavigationAdapter = {
    sessionKey: "tab-a", pathname: `/test/issues/${current.id}`,
    searchParams: new URLSearchParams(withOrigin ? "review=source-token" : ""), hash: "",
    push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: (path) => path,
  };
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={getTestResources()}>
        <QueryClientProvider client={qc}>
          <NavigationProvider value={adapter}>
            <ReviewDetailContext.Provider value={current.id}>{children}</ReviewDetailContext.Provider>
          </NavigationProvider>
        </QueryClientProvider>
      </I18nProvider>
    );
  }
  const hook = renderHook(() => {
    const complete = useReviewCompletion();
    const mutation = useUpdateIssue();
    return { complete: (updates: Partial<UpdateIssueRequest> = { status: "done" }) =>
      complete(current, updates, "done", (input) => mutation.mutateAsync(input)),
    session: useContext(ReviewSessionContext)!, navigation: useNavigation() };
  }, { wrapper: Wrapper });
  if (withOrigin) hook.result.current.session.origin = {
    token: "source-token", workspaceId: "ws", userId: "member", listPath: "/test/issues?sort=title",
    query, sourceKey: "list", scroll: [],
  };
  return { ...hook, adapter, current };
}

function undo() {
  const options = vi.mocked(toast.success).mock.calls[0]![1] as unknown as { action: { onClick: () => void } };
  act(() => options.action.onClick());
}

describe("completion with the real navigation provider and HTTP client", () => {
  it("reads unloaded pages, advances by identifier, and Undoes with the committed revision", async () => {
    const hook = setup();
    act(() => expect(hook.result.current.complete()).toBe(true));
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenCalledExactlyOnceWith("/test/issues/TEST-51?review=source-token"));
    expect(writes).toEqual([{ status: "done", expected_revision: 7 }]);
    const reads = requests.filter((request) => request.path.includes("/table/"));
    expect(reads.every((request) => JSON.stringify(request.body.query) === JSON.stringify(query))).toBe(true);
    expect(reads.filter((request) => request.path.endsWith("/rows")).map((request) => request.body.page)).toEqual([
      { limit: 50, cursor: null }, { limit: 50, cursor: "50" },
      { limit: 50, cursor: null }, { limit: 50, cursor: "50" },
    ]);
    undo();
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenLastCalledWith("/test/issues/TEST-50?review=source-token"));
    expect(writes).toEqual([
      { status: "done", expected_revision: 7 },
      { status: "in_review", expected_revision: 8, suppress_run: true },
    ]);
    expect(issues[49]).toMatchObject({ status: "in_review", revision: 9 });
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("returns the last item to its source list and keeps Undo available there", async () => {
    issues = [issue(0)];
    const hook = setup(issues[0]);
    act(() => hook.result.current.complete());
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenCalledExactlyOnceWith("/test/issues?sort=title"));
    undo();
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenLastCalledWith("/test/issues/TEST-1?review=source-token"));
    expect(writes[1]).toEqual({ status: "in_review", expected_revision: 8, suppress_run: true });
  });

  it.each([
    { status: 409, code: "revision_conflict", message: "Concurrent edit" },
    { status: 400, code: "workflow_transition_invalid", message: "Workflow refused completion" },
  ])("keeps the current route when HTTP $status rejects completion", async (failure) => {
    rejectWrite = failure;
    const hook = setup();
    act(() => hook.result.current.complete());
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(writes).toEqual([{ status: "done", expected_revision: 7 }]);
    expect(issues[49]).toMatchObject({ status: "in_review", revision: 7 });
    expect(hook.adapter.replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("does not reclaim navigation from a manually left detail after a held successful response", async () => {
    heldWrite = new Promise<void>((resolve) => { releaseWrite = resolve; });
    const hook = setup();
    act(() => hook.result.current.complete());
    await waitFor(() => expect(writes).toHaveLength(1));
    act(() => hook.result.current.navigation.push("/test/agents"));
    releaseWrite!();
    await waitFor(() => expect(hook.result.current.session.busy).toBe(false));
    expect(issues[49]).toMatchObject({ status: "done", revision: 8 });
    expect(hook.adapter.push).toHaveBeenCalledExactlyOnceWith("/test/agents");
    expect(hook.adapter.replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("expires an old Undo through the provider's manual-navigation lease", async () => {
    const hook = setup();
    act(() => hook.result.current.complete());
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenCalledTimes(1));
    act(() => hook.result.current.navigation.push("/test/agents"));
    undo();
    expect(writes).toEqual([{ status: "done", expected_revision: 7 }]);
    expect(hook.adapter.replace).toHaveBeenCalledTimes(1);
    expect(hook.result.current.session.receipt).toBeGreaterThan(1);
  });

  it("isolates a new logical tab from a previous tab's held completion", async () => {
    heldWrite = new Promise<void>((resolve) => { releaseWrite = resolve; });
    const hook = setup();
    const oldSession = hook.result.current.session;
    act(() => hook.result.current.complete());
    await waitFor(() => expect(writes).toHaveLength(1));
    hook.adapter.sessionKey = "tab-b";
    hook.rerender();
    expect(hook.result.current.session).not.toBe(oldSession);
    expect(hook.result.current.session.origin).toBeNull();
    releaseWrite!();
    await waitFor(() => expect(oldSession.busy).toBe(false));
    expect(oldSession.origin).toBeNull();
    expect(hook.adapter.replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("accepts a repeated click without issuing a second completion", async () => {
    heldWrite = new Promise<void>((resolve) => { releaseWrite = resolve; });
    const hook = setup();
    act(() => {
      expect(hook.result.current.complete()).toBe(true);
      expect(hook.result.current.complete()).toBe(true);
    });
    await waitFor(() => expect(writes).toHaveLength(1));
    releaseWrite!();
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenCalledTimes(1));
    expect(writes).toEqual([{ status: "done", expected_revision: 7 }]);
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("does not navigate after its provider unmounts while the successful response is held", async () => {
    heldWrite = new Promise<void>((resolve) => { releaseWrite = resolve; });
    const hook = setup();
    const oldSession = hook.result.current.session;
    act(() => hook.result.current.complete());
    await waitFor(() => expect(writes).toHaveLength(1));
    hook.unmount();
    releaseWrite!();
    await waitFor(() => expect(oldSession.busy).toBe(false));
    expect(oldSession.origin).toBeNull();
    expect(hook.adapter.replace).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("keeps the concurrent state and route when Undo receives HTTP 409", async () => {
    const hook = setup();
    act(() => hook.result.current.complete());
    await waitFor(() => expect(hook.adapter.replace).toHaveBeenCalledTimes(1));
    issues[49] = { ...issues[49]!, status: "blocked", revision: 9 };
    undo();
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(writes[1]).toEqual({ status: "in_review", expected_revision: 8, suppress_run: true });
    expect(issues[49]).toMatchObject({ status: "blocked", revision: 9 });
    expect(hook.adapter.replace).toHaveBeenCalledTimes(1);
  });

  it("declines direct links and unrelated field edits without reading or writing a queue", () => {
    const direct = setup(issues[49], false);
    act(() => expect(direct.result.current.complete()).toBe(false));
    const review = setup();
    act(() => expect(review.result.current.complete({ status: "done", priority: "high" })).toBe(false));
    expect(requests).toEqual([]);
    expect(writes).toEqual([]);
    expect(review.adapter.replace).not.toHaveBeenCalled();
  });
});
