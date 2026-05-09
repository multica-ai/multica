import { describe, it, expect, beforeEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { ReactNode } from "react";

// Mock the api singleton BEFORE importing the queries module so the mutation
// hooks pick up the spy.
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      mergePullRequest: vi.fn(),
      closePullRequestAsStale: vi.fn(),
      rebasePullRequestOnMain: vi.fn(),
      commentOnPullRequest: vi.fn(),
      dismissPullRequestReview: vi.fn(),
      diagnoseCIFailure: vi.fn(),
      summarizeReviewFeedback: vi.fn(),
      nudgePullRequestAuthor: vi.fn(),
      runSmokeTests: vi.fn(),
      listShipCardActions: vi.fn(),
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

import { api } from "@multica/core/api";
import {
  shipKeys,
  useMergePullRequest,
  useClosePullRequestAsStale,
  useNudgePullRequestAuthor,
} from "@multica/core/ship";
import type {
  ActionResult,
  ListPullRequestsResponse,
  PullRequest,
} from "@multica/core/types";

const wsId = "ws-1";

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: wsId,
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 1,
    title: "Test",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/1",
    body: null,
    ci_status: "success",
    review_decision: "APPROVED",
    mergeable: "MERGEABLE",
    additions: 1,
    deletions: 0,
    changed_files: 1,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-08T00:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T00:00:00Z",
    ...overrides,
  };
}

function makeWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("useMergePullRequest optimistic update", () => {
  it("flips the cached PR to merged before the request settles", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const projectId = "p-1";
    qc.setQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
      { pull_requests: [makePR()], total: 1 },
    );

    // Server response is delayed so we can observe the optimistic patch
    // before the await resolves.
    let resolveServer: (v: ActionResult) => void = () => {};
    vi.mocked(api.mergePullRequest).mockReturnValue(
      new Promise<ActionResult>((res) => {
        resolveServer = res;
      }),
    );

    const { result } = renderHook(() => useMergePullRequest("pr-1"), {
      wrapper: makeWrapper(qc),
    });

    let mutPromise!: Promise<unknown>;
    act(() => {
      mutPromise = result.current.mutateAsync(undefined);
    });

    await waitFor(() => {
      const cached = qc.getQueryData<ListPullRequestsResponse>(
        shipKeys.pullRequests(wsId, projectId, "open"),
      );
      expect(cached?.pull_requests[0]?.state).toBe("merged");
      expect(cached?.pull_requests[0]?.pr_merged_at).toBeTruthy();
    });

    resolveServer({
      status: "succeeded",
      action_id: "act-1",
      merge_sha: "abc123",
    });
    await mutPromise;
  });

  it("rolls back the cache when the server rejects the merge", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const projectId = "p-1";
    qc.setQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
      { pull_requests: [makePR()], total: 1 },
    );

    vi.mocked(api.mergePullRequest).mockRejectedValue(
      new Error("branch is not mergeable"),
    );

    const { result } = renderHook(() => useMergePullRequest("pr-1"), {
      wrapper: makeWrapper(qc),
    });

    await expect(result.current.mutateAsync(undefined)).rejects.toThrow(
      "branch is not mergeable",
    );

    const cached = qc.getQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
    );
    // Snapshot restored — state is back to "open" (the optimistic flip
    // is undone by the onError rollback).
    expect(cached?.pull_requests[0]?.state).toBe("open");
    expect(cached?.pull_requests[0]?.pr_merged_at).toBeNull();
  });
});

describe("useClosePullRequestAsStale optimistic update", () => {
  it("flips state to closed on optimistic mount", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const projectId = "p-1";
    qc.setQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
      { pull_requests: [makePR()], total: 1 },
    );

    vi.mocked(api.closePullRequestAsStale).mockResolvedValue({
      status: "succeeded",
      action_id: "act-1",
    });

    const { result } = renderHook(
      () => useClosePullRequestAsStale("pr-1"),
      { wrapper: makeWrapper(qc) },
    );

    await result.current.mutateAsync({ reason: "stale" });

    const cached = qc.getQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
    );
    expect(cached?.pull_requests[0]?.state).toBe("closed");
  });
});

describe("useNudgePullRequestAuthor (no optimistic update)", () => {
  it("does not patch the cache on mutate", async () => {
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const projectId = "p-1";
    qc.setQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
      { pull_requests: [makePR()], total: 1 },
    );

    vi.mocked(api.nudgePullRequestAuthor).mockResolvedValue({
      status: "succeeded",
      action_id: "act-1",
    });

    const { result } = renderHook(
      () => useNudgePullRequestAuthor("pr-1"),
      { wrapper: makeWrapper(qc) },
    );

    await result.current.mutateAsync(undefined);

    const cached = qc.getQueryData<ListPullRequestsResponse>(
      shipKeys.pullRequests(wsId, projectId, "open"),
    );
    // Cache stays untouched (until the WS event triggers a refetch).
    expect(cached?.pull_requests[0]?.state).toBe("open");
  });
});
