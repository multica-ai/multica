// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { inboxKeys } from "@/data/queries/inbox";
import { useMarkInboxReadOnOpen } from "./use-mark-inbox-read-on-open";

const hook = vi.hoisted(() => ({
  client: undefined as unknown,
  summary: { data: 0 as number | undefined, isSuccess: true, isFetching: false },
  effect: undefined as (() => void) | undefined,
  ref: { current: null as string | null },
  markRead: vi.fn(),
}));

// Run the hook's decision headlessly: the effect is captured and invoked by
// the test, the summary query's state is set per case, and the list cache is
// a real QueryClient so staleness and invalidation behave as in the app.
vi.mock("react", () => ({
  useEffect: (effect: () => void) => { hook.effect = effect; },
  useRef: () => hook.ref,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => hook.summary,
  useQueryClient: () => hook.client,
}));
vi.mock("@/data/mutations/inbox", () => ({
  useMarkIssueInboxRead: () => ({ mutate: hook.markRead }),
}));
vi.mock("@/data/api", () => ({ api: {} }));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "member",
    actor_id: "member-2",
    type: "mentioned",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue",
    body: null,
    issue_status: null,
    section: "action",
    read: false,
    archived: false,
    created_at: "2026-11-02T08:00:00Z",
    details: null,
    ...overrides,
  };
}

let client: QueryClient;

const runEffect = () => hook.effect?.();

describe("useMarkInboxReadOnOpen", () => {
  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    hook.client = client;
    hook.summary = { data: 1, isSuccess: true, isFetching: false };
    hook.ref.current = null;
  });
  afterEach(() => {
    client.clear();
    hook.markRead.mockReset();
  });

  it("marks the opened issue read when its unread row is cached", () => {
    client.setQueryData(inboxKeys.list("ws-1"), [item()]);
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).toHaveBeenCalledWith("issue-1");
  });

  it("skips when a fresh list holds only activity or read rows for the issue", () => {
    client.setQueryData(inboxKeys.list("ws-1"), [
      item({ id: "status", type: "status_changed", section: "activity" }),
      item({ id: "read", read: true }),
    ]);
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).not.toHaveBeenCalled();
  });

  it("ignores an invalidated list that only holds the issue's old read row", async () => {
    client.setQueryData(inboxKeys.list("ws-1"), [item({ read: true })]);
    await client.invalidateQueries({ queryKey: inboxKeys.list("ws-1"), refetchType: "none" });
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).toHaveBeenCalledWith("issue-1");
  });

  it("waits for a refetching zero summary instead of consuming the opening", () => {
    hook.summary = { data: 0, isSuccess: true, isFetching: true };
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).not.toHaveBeenCalled();
    expect(hook.ref.current).toBeNull();

    hook.summary = { data: 1, isSuccess: true, isFetching: false };
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).toHaveBeenCalledWith("issue-1");
  });

  it("skips once the settled summary reports nothing unread", () => {
    hook.summary = { data: 0, isSuccess: true, isFetching: false };
    useMarkInboxReadOnOpen("ws-1", "issue-1");
    runEffect();
    expect(hook.markRead).not.toHaveBeenCalled();
  });
});
