import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockOpenModal = vi.fn();
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: any) => {
      const state = { open: mockOpenModal };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

const mockAuthState = { user: { id: "user-1" }, isAuthenticated: true };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => (selector ? selector(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  ),
  registerAuthStore: vi.fn(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () =>
      Promise.resolve([
        { user_id: "user-1", name: "Test User", email: "t@t.com", role: "admin" },
      ]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
}));

// Mutable so individual tests can seed the pin list.
const pinListRef: { value: Array<{ item_type: string; item_id: string }> } = {
  value: [],
};
const mockCreatePinMutate = vi.fn();
const mockDeletePinMutate = vi.fn();
vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({
    queryKey: ["pins", "ws-1", "user-1"],
    queryFn: () => Promise.resolve(pinListRef.value),
  }),
  useCreatePin: () => ({ mutate: mockCreatePinMutate }),
  useDeletePin: () => ({ mutate: mockDeletePinMutate }),
}));

const mockUpdateMutate = vi.fn();
vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: mockUpdateMutate }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/issues/issue-1",
    searchParams: new URLSearchParams(),
    back: vi.fn(),
    replace: vi.fn(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@multica/i18n/react", () => ({
  useT: () => (key: string, params?: Record<string, string | number>) => {
    if (params) return `${key} ${JSON.stringify(params)}`;
    return key;
  },
}));

// Import AFTER mocks are registered.
import { useIssueActions } from "../use-issue-actions";

const mockIssue: Issue = {
  id: "issue-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Issue;

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mockOpenModal.mockReset();
  mockUpdateMutate.mockReset();
  mockCreatePinMutate.mockReset();
  mockDeletePinMutate.mockReset();
  pinListRef.value = [];
  localStorage.clear();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

describe("useIssueActions", () => {
  it("updateField dispatches useUpdateIssue.mutate with the correct payload", () => {
    const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });

    act(() => {
      result.current.updateField({ status: "done" });
    });

    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "issue-1", status: "done" },
      expect.any(Object),
    );
  });

  it("assigning an agent to a backlog issue opens the backlog-hint modal", () => {
    const backlogIssue = { ...mockIssue, status: "backlog" } as Issue;
    const { result } = renderHook(() => useIssueActions(backlogIssue), { wrapper });

    act(() => {
      result.current.updateField({
        assignee_type: "agent",
        assignee_id: "agent-1",
      });
    });

    expect(mockOpenModal).toHaveBeenCalledWith("issue-backlog-agent-hint", {
      issueId: "issue-1",
    });
  });

  it("does not re-open backlog-hint when the user has dismissed it", () => {
    localStorage.setItem("multica:backlog-agent-hint-dismissed", "true");
    const backlogIssue = { ...mockIssue, status: "backlog" } as Issue;
    const { result } = renderHook(() => useIssueActions(backlogIssue), { wrapper });

    act(() => {
      result.current.updateField({
        assignee_type: "agent",
        assignee_id: "agent-1",
      });
    });

    expect(mockOpenModal).not.toHaveBeenCalled();
  });

  it("copyLink writes the issue's shareable URL to the clipboard", async () => {
    const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });

    await act(async () => {
      await result.current.copyLink();
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      "https://app.multica.com/test/issues/issue-1",
    );
  });

  it("openSetParent / openAddChild / openDeleteConfirm / openCreateSubIssue open the correct modal with payload", () => {
    const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });

    act(() => {
      result.current.openSetParent();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("issue-set-parent", {
      issueId: "issue-1",
    });

    act(() => {
      result.current.openAddChild();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("issue-add-child", {
      issueId: "issue-1",
    });

    act(() => {
      result.current.openCreateSubIssue();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("create-issue", {
      parent_issue_id: "issue-1",
      parent_issue_identifier: "TES-1",
    });

    act(() => {
      result.current.openDeleteConfirm({ onDeletedNavigateTo: "/test/issues" });
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("issue-delete-confirm", {
      issueId: "issue-1",
      identifier: "TES-1",
      onDeletedNavigateTo: "/test/issues",
    });
  });

  it("togglePin calls createPin when not pinned and deletePin when pinned", async () => {
    pinListRef.value = [];
    const { result: r1 } = renderHook(() => useIssueActions(mockIssue), { wrapper });
    await waitFor(() => {
      expect(r1.current.isPinned).toBe(false);
    });
    act(() => {
      r1.current.togglePin();
    });
    expect(mockCreatePinMutate).toHaveBeenCalledWith({
      item_type: "issue",
      item_id: "issue-1",
    });
    expect(mockDeletePinMutate).not.toHaveBeenCalled();

    mockCreatePinMutate.mockReset();
    mockDeletePinMutate.mockReset();
    pinListRef.value = [{ item_type: "issue", item_id: "issue-1" }];
    const { result: r2 } = renderHook(() => useIssueActions(mockIssue), { wrapper });
    await waitFor(() => {
      expect(r2.current.isPinned).toBe(true);
    });
    act(() => {
      r2.current.togglePin();
    });
    expect(mockDeletePinMutate).toHaveBeenCalledWith({
      itemType: "issue",
      itemId: "issue-1",
    });
    expect(mockCreatePinMutate).not.toHaveBeenCalled();
  });

  it("is a safe no-op when issue is null", () => {
    const { result } = renderHook(() => useIssueActions(null), { wrapper });

    act(() => {
      result.current.updateField({ status: "done" });
      result.current.togglePin();
      result.current.openSetParent();
    });

    expect(mockUpdateMutate).not.toHaveBeenCalled();
    expect(mockOpenModal).not.toHaveBeenCalled();
  });

  it("members and filtered agents are exposed on the result", async () => {
    const { result } = renderHook(() => useIssueActions(mockIssue), { wrapper });
    await waitFor(() => {
      expect(result.current.members.length).toBe(1);
    });
    expect(result.current.members[0]!.user_id).toBe("user-1");
    expect(result.current.agents).toEqual([]);
  });
});
