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
  useToggleCommentReaction: () => ({
    mutateAsync: vi.fn(),
    mutate: stableHandles.toggleMutate,
    isPending: false,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueTimelineInfiniteOptions: (id: string, around?: string | null) => ({
    queryKey: around
      ? ["issues", "timeline", id, "around", around]
      : ["issues", "timeline", id],
    queryFn: () => Promise.resolve(emptyPage()),
    initialPageParam: { mode: "latest" as const },
    getNextPageParam: () => undefined,
    getPreviousPageParam: () => undefined,
  }),
  issueKeys: {
    timeline: (id: string, around?: string | null) =>
      around
        ? ["issues", "timeline", id, "around", around]
        : ["issues", "timeline", id],
  },
}));

// Hoisted state controllable from tests — represents what useInfiniteQuery
// would return for the current render.
const queryState = vi.hoisted(() => ({
  // by default: at-latest with one page that has no newer entries.
  data: undefined as unknown,
  isLoading: false,
}));

function emptyPage() {
  return {
    entries: [],
    next_cursor: null,
    prev_cursor: null,
    has_more_before: false,
    has_more_after: false,
  };
}

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useInfiniteQuery: () => ({
      data: queryState.data,
      isLoading: queryState.isLoading,
      fetchNextPage: vi.fn(),
      fetchPreviousPage: vi.fn(),
      hasNextPage: false,
      hasPreviousPage: false,
      isFetchingNextPage: false,
      isFetchingPreviousPage: false,
    }),
    useQueryClient: () => ({
      invalidateQueries: vi.fn(),
      setQueryData: vi.fn(),
      setQueriesData: vi.fn(),
      getQueryData: vi.fn(),
      getQueriesData: vi.fn(() => []),
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
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: false }],
      pageParams: [{ mode: "latest" }],
    };
    queryState.isLoading = false;
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

  it("flattens DESC pages into ASC timeline order", () => {
    queryState.data = {
      pages: [
        // Latest page: DESC.
        {
          ...emptyPage(),
          entries: [
            { type: "comment", id: "c3", actor_type: "member", actor_id: "u", created_at: "2026-05-06T03:00:00Z" },
            { type: "comment", id: "c2", actor_type: "member", actor_id: "u", created_at: "2026-05-06T02:00:00Z" },
          ],
          has_more_after: false,
        },
        // Older page: also DESC.
        {
          ...emptyPage(),
          entries: [
            { type: "comment", id: "c1", actor_type: "member", actor_id: "u", created_at: "2026-05-06T01:00:00Z" },
          ],
        },
      ],
      pageParams: [{ mode: "latest" }, { mode: "before", cursor: "x" }],
    };
    const { result } = renderHook(() => useIssueTimeline("issue-1", "user-1"));
    const ids = result.current.timeline.map((e) => e.id);
    // ASC: oldest at top, newest at bottom.
    expect(ids).toEqual(["c1", "c2", "c3"]);
  });

  it("reports isAtLatest=true when first page has no newer entries", () => {
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: false }],
      pageParams: [{ mode: "latest" }],
    };
    const { result } = renderHook(() => useIssueTimeline("issue-1", "user-1"));
    expect(result.current.isAtLatest).toBe(true);
    expect(result.current.newEntriesBelowCount).toBe(0);
  });

  it("bumps newEntriesBelowCount when comment:created arrives while not at latest", () => {
    // Around-mode page: the user is reading older history, so has_more_after=true.
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: true }],
      pageParams: [{ mode: "around", id: "anchor" }],
    };
    const { result } = renderHook(() =>
      useIssueTimeline("issue-1", "user-1", { around: "anchor" }),
    );
    expect(result.current.isAtLatest).toBe(false);
    expect(result.current.newEntriesBelowCount).toBe(0);

    const handler = wsHandlers.get("comment:created");
    expect(handler).toBeDefined();
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
    expect(result.current.newEntriesBelowCount).toBe(1);
  });

  it("does NOT bump newEntriesBelowCount when at-latest (entry should land in cache instead)", () => {
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: false }],
      pageParams: [{ mode: "latest" }],
    };
    const { result } = renderHook(() => useIssueTimeline("issue-1", "user-1"));
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
    expect(result.current.newEntriesBelowCount).toBe(0);
  });

  it("ignores WS events for other issues", () => {
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: true }],
      pageParams: [{ mode: "around", id: "anchor" }],
    };
    const { result } = renderHook(() =>
      useIssueTimeline("issue-1", "user-1", { around: "anchor" }),
    );
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
    expect(result.current.newEntriesBelowCount).toBe(0);
  });

  it("jumpToLatest clears newEntriesBelowCount", () => {
    queryState.data = {
      pages: [{ ...emptyPage(), has_more_after: true }],
      pageParams: [{ mode: "around", id: "anchor" }],
    };
    const { result } = renderHook(() =>
      useIssueTimeline("issue-1", "user-1", { around: "anchor" }),
    );
    const handler = wsHandlers.get("comment:created");
    act(() => {
      handler!({
        comment: {
          id: "n",
          issue_id: "issue-1",
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
    expect(result.current.newEntriesBelowCount).toBe(1);
    act(() => {
      result.current.jumpToLatest();
    });
    expect(result.current.newEntriesBelowCount).toBe(0);
  });
});
