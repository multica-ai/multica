import React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@/shared/types";
import { queryKeys } from "@/shared/query";
import { useIssueMutations } from "./mutations";

const apiMocks = vi.hoisted(() => ({
  createIssue: vi.fn(),
  updateIssue: vi.fn(),
  deleteIssue: vi.fn(),
  batchUpdateIssues: vi.fn(),
  batchDeleteIssues: vi.fn(),
  createComment: vi.fn(),
  updateComment: vi.fn(),
  deleteComment: vi.fn(),
  removeReaction: vi.fn(),
  addReaction: vi.fn(),
  subscribeToIssue: vi.fn(),
  unsubscribeFromIssue: vi.fn(),
  removeIssueReaction: vi.fn(),
  addIssueReaction: vi.fn(),
  cancelTask: vi.fn(),
}));

vi.mock("@/shared/api", () => ({
  api: apiMocks,
}));

vi.mock("@/features/workspace", () => ({
  useWorkspaceStore: (selector: (state: { workspace: { id: string } | null }) => unknown) =>
    selector({ workspace: { id: "ws-1" } }),
}));

function createIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "TES-1",
    title: "Original title",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    due_date: null,
    start_date: null,
    end_date: null,
    reactions: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("useIssueMutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("patches issue caches optimistically during update", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const issue = createIssue();
    queryClient.setQueryData(queryKeys.issues.list("ws-1", { limit: 200 }), {
      issues: [issue],
      total: 1,
    });
    queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);

    let resolveUpdate: ((value: Issue) => void) | null = null;
    apiMocks.updateIssue.mockReturnValue(
      new Promise<Issue>((resolve) => {
        resolveUpdate = resolve;
      }),
    );

    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useIssueMutations(), { wrapper });

    let mutationPromise: Promise<Issue>;
    await act(async () => {
      mutationPromise = result.current.updateIssue(issue.id, { title: "Updated title" }) as Promise<Issue>;
      await Promise.resolve();
    });

    expect(
      queryClient.getQueryData<{ issues: Issue[]; total: number }>(queryKeys.issues.list("ws-1", { limit: 200 }))?.issues[0]?.title,
    ).toBe("Updated title");
    expect(
      queryClient.getQueryData<Issue>(queryKeys.issues.detail(issue.id))?.title,
    ).toBe("Updated title");

    await act(async () => {
      resolveUpdate?.(createIssue({ title: "Updated title" }));
      await mutationPromise!;
    });
  });

  it("adds created issues into the workspace issue list cache", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const existingIssue = createIssue();
    const nextIssue = createIssue({
      id: "issue-2",
      number: 2,
      identifier: "TES-2",
      title: "Created issue",
      position: 2,
    });

    queryClient.setQueryData(queryKeys.issues.list("ws-1", { limit: 200 }), {
      issues: [existingIssue],
      total: 1,
    });
    apiMocks.createIssue.mockResolvedValue(nextIssue);

    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useIssueMutations(), { wrapper });

    await act(async () => {
      await result.current.createIssue({ title: "Created issue" });
    });

    expect(
      queryClient.getQueryData<{ issues: Issue[]; total: number }>(queryKeys.issues.list("ws-1", { limit: 200 }))?.issues,
    ).toEqual([existingIssue, nextIssue]);
    expect(queryClient.getQueryData<Issue>(queryKeys.issues.detail(nextIssue.id))).toEqual(nextIssue);
  });
});
