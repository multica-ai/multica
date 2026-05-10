import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";

// Mock @multica/core/issues/mutations to mimic TanStack Query v5's contract:
// useMutation returns a fresh result wrapper on every render, but the
// `mutate` / `mutateAsync` functions inside it are stable across renders.
// This is exactly the shape that previously fooled the original deps lists
// in useIssueTimeline — guarding against a regression here means future code
// can't accidentally pull the whole mutation result into a useCallback dep.
const stableHandles = vi.hoisted(() => ({
  createMutateAsync: vi.fn(async () => ({})),
  updateMutateAsync: vi.fn(async () => ({})),
  deleteMutateAsync: vi.fn(async () => ({})),
  resolveMutateAsync: vi.fn(async () => ({})),
  toggleMutate: vi.fn(),
}));

// WS event registry — captured handlers per event name so tests can simulate
// server pushes by invoking them directly.
const wsHandlers = vi.hoisted(() => new Map<string, (payload: unknown) => void>());

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateComment: () => ({
    mutateAsync: stableHandles.createMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useUpdateComment: () => ({
    mutateAsync: stableHandles.updateMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useDeleteComment: () => ({
    mutateAsync: stableHandles.deleteMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useResolveComment: () => ({
    mutateAsync: stableHandles.resolveMutateAsync,
    mutate: vi.fn(),
    isPending: false,
  }),
  useToggleCommentReaction: () => ({
    mutateAsync: vi.fn(),
    mutate: stableHandles.toggleMutate,
    isPending: false,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueTimelineOptions: (id: string) => ({
    queryKey: ["issues", "timeline", id],
    queryFn: () => Promise.resolve([]),
  }),
  issueKeys: {
    timeline: (id: string) => ["issues", "timeline", id],
  },
}));

// Hoisted state controllable from tests — represents what useQuery would
// return for the current render.
const queryState = vi.hoisted(() => ({
  data: undefined as unknown,
  isLoading: false,
}));

// Track the latest cache-update fn the hook hands to setQueryData so tests
// can assert what would have been written.
const cacheUpdates = vi.hoisted(() => ({
  last: null as unknown,
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: () => ({
      data: queryState.data,
      isLoading: queryState.isLoading,
    }),
    useQueryClient: () => ({
      invalidateQueries: vi.fn(),
      setQueryData: vi.fn((_key: unknown, updater: unknown) => {
        cacheUpdates.last = typeof updater === "function"
          ? (updater as (old: unknown) => unknown)(queryState.data)
          : updater;
      }),
      getQueryData: vi.fn(),
      cancelQueries: vi.fn(),
    }),
    useMutationState: () => [],
  };
});

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsHandlers.set(event, handler);
  },
  useWSReconnect: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { useIssueTimeline } from "./use-issue-timeline";

describe("useIssueTimeline", () => {
  beforeEach(() => {
    wsHandlers.clear();
    queryState.data = [];
    queryState.isLoading = false;
    cacheUpdates.last = null;
  });

  // CommentCard is wrapped in React.memo (perf fix for long timelines, see
  // multica#1968). The memo only pays off if the callbacks passed down keep
  // the same identity across unrelated parent re-renders. TanStack Query v5
  // returns a *new* mutation result wrapper on every render, so a useCallback
  // listing the whole mutation object as a dep flips its identity every time
  // — that is the exact regression this test guards against.
  it("submitReply / editComment / deleteComment / toggleReaction keep identity across unrelated re-renders", () => {
    const { result, rerender } = renderHook(() => useIssueTimeline("issue-1", "user-1"));

    const first = {
      submitComment: result.current.submitComment,
      submitReply: result.current.submitReply,
      editComment: result.current.editComment,
      deleteComment: result.current.deleteComment,
      toggleReaction: result.current.toggleReaction,
    };

    rerender();
    rerender();

    expect(result.current.submitReply).toBe(first.submitReply);
    expect(result.current.editComment).toBe(first.editComment);
    expect(result.current.deleteComment).toBe(first.deleteComment);
    expect(result.current.toggleReaction).toBe(first.toggleReaction);
    expect(result.current.submitComment).toBe(first.submitComment);
  });

  it("returns the timeline as a flat array directly from the query cache", () => {
    queryState.data = [
      { type: "comment", id: "c1", actor_type: "member", actor_id: "u", created_at: "2026-05-06T01:00:00Z" },
      { type: "comment", id: "c2", actor_type: "member", actor_id: "u", created_at: "2026-05-06T02:00:00Z" },
      { type: "comment", id: "c3", actor_type: "member", actor_id: "u", created_at: "2026-05-06T03:00:00Z" },
    ];
    const { result } = renderHook(() => useIssueTimeline("issue-1", "user-1"));
    expect(result.current.timeline.map((e) => e.id)).toEqual(["c1", "c2", "c3"]);
  });

  it("comment:created appends the new entry to the cache", () => {
    queryState.data = [];
    renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const handler = wsHandlers.get("comment:created");
    act(() => {
      handler!({
        comment: {
          id: "new-c",
          issue_id: "issue-1",
          author_type: "member",
          author_id: "u",
          content: "hi",
          parent_id: null,
          created_at: "2026-05-06T05:00:00Z",
          updated_at: "2026-05-06T05:00:00Z",
          type: "comment",
          reactions: [],
          attachments: [],
        },
      });
    });
    const updated = cacheUpdates.last as Array<{ id: string }>;
    expect(updated.map((e) => e.id)).toEqual(["new-c"]);
  });

  it("ignores WS events for other issues", () => {
    queryState.data = [];
    renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const handler = wsHandlers.get("comment:created");
    act(() => {
      handler!({
        comment: {
          id: "x",
          issue_id: "different-issue",
          author_type: "member",
          author_id: "u",
          content: "",
          parent_id: null,
          created_at: "",
          updated_at: "",
          type: "comment",
          reactions: [],
          attachments: [],
        },
      });
    });
    // setQueryData should not have been invoked for a non-matching issue.
    expect(cacheUpdates.last).toBeNull();
  });

  // The global useRealtimeSync handler now uses refetchType: "none" for
  // timeline events, which means useIssueTimeline must own the granular
  // cache update for every event that mutates the timeline — including
  // comment:resolved / comment:unresolved. Without these handlers the
  // resolve toggle on a thread root would only update the cache when the
  // user remounts IssueDetail (the stale flag triggers a refetch), so the
  // bar/expanded view would lag the click by a navigation cycle.
  it("comment:resolved updates the matching entry in place with the new resolved fields", () => {
    queryState.data = [
      {
        type: "comment",
        id: "c1",
        actor_type: "member",
        actor_id: "u",
        content: "hello",
        parent_id: null,
        created_at: "2026-05-06T01:00:00Z",
        updated_at: "2026-05-06T01:00:00Z",
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
      {
        type: "comment",
        id: "c2",
        actor_type: "member",
        actor_id: "u",
        content: "untouched",
        parent_id: null,
        created_at: "2026-05-06T02:00:00Z",
        updated_at: "2026-05-06T02:00:00Z",
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
    ];
    renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const handler = wsHandlers.get("comment:resolved");
    expect(handler).toBeDefined();
    act(() => {
      handler!({
        comment: {
          id: "c1",
          issue_id: "issue-1",
          author_type: "member",
          author_id: "u",
          content: "hello",
          parent_id: null,
          created_at: "2026-05-06T01:00:00Z",
          updated_at: "2026-05-06T01:00:00Z",
          type: "comment",
          reactions: [],
          attachments: [],
          resolved_at: "2026-05-06T03:00:00Z",
          resolved_by_type: "member",
          resolved_by_id: "u",
        },
      });
    });
    const updated = cacheUpdates.last as Array<{
      id: string;
      resolved_at: string | null;
      resolved_by_type: string | null;
      resolved_by_id: string | null;
    }>;
    expect(updated.map((e) => e.id)).toEqual(["c1", "c2"]);
    expect(updated[0]!.resolved_at).toBe("2026-05-06T03:00:00Z");
    expect(updated[0]!.resolved_by_type).toBe("member");
    expect(updated[0]!.resolved_by_id).toBe("u");
    // Sibling entry must not change (identity preserved by .map).
    expect(updated[1]!.resolved_at).toBeNull();
  });

  it("comment:unresolved clears the resolved fields on the matching entry", () => {
    queryState.data = [
      {
        type: "comment",
        id: "c1",
        actor_type: "member",
        actor_id: "u",
        content: "hello",
        parent_id: null,
        created_at: "2026-05-06T01:00:00Z",
        updated_at: "2026-05-06T01:00:00Z",
        reactions: [],
        attachments: [],
        resolved_at: "2026-05-06T03:00:00Z",
        resolved_by_type: "member",
        resolved_by_id: "u",
      },
    ];
    renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const handler = wsHandlers.get("comment:unresolved");
    expect(handler).toBeDefined();
    act(() => {
      handler!({
        comment: {
          id: "c1",
          issue_id: "issue-1",
          author_type: "member",
          author_id: "u",
          content: "hello",
          parent_id: null,
          created_at: "2026-05-06T01:00:00Z",
          updated_at: "2026-05-06T01:00:00Z",
          type: "comment",
          reactions: [],
          attachments: [],
          resolved_at: null,
          resolved_by_type: null,
          resolved_by_id: null,
        },
      });
    });
    const updated = cacheUpdates.last as Array<{
      id: string;
      resolved_at: string | null;
    }>;
    expect(updated[0]!.resolved_at).toBeNull();
  });

  it("comment:resolved ignores events from other issues", () => {
    queryState.data = [
      {
        type: "comment",
        id: "c1",
        actor_type: "member",
        actor_id: "u",
        content: "hello",
        parent_id: null,
        created_at: "2026-05-06T01:00:00Z",
        updated_at: "2026-05-06T01:00:00Z",
        reactions: [],
        attachments: [],
        resolved_at: null,
        resolved_by_type: null,
        resolved_by_id: null,
      },
    ];
    renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const handler = wsHandlers.get("comment:resolved");
    act(() => {
      handler!({
        comment: {
          id: "c1",
          issue_id: "different-issue",
          author_type: "member",
          author_id: "u",
          content: "hello",
          parent_id: null,
          created_at: "2026-05-06T01:00:00Z",
          updated_at: "2026-05-06T01:00:00Z",
          type: "comment",
          reactions: [],
          attachments: [],
          resolved_at: "2026-05-06T03:00:00Z",
          resolved_by_type: "member",
          resolved_by_id: "u",
        },
      });
    });
    expect(cacheUpdates.last).toBeNull();
  });
});
