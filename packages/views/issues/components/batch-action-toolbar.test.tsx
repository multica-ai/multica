// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";

const selectionState = vi.hoisted(() => ({
  selectedIds: new Set<string>(),
  clear: vi.fn(),
}));
const mockBatchUpdateIssues = vi.hoisted(() => vi.fn());
const mockBatchDeleteIssues = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: any) => {
    const state = { user: { id: "user-1", name: "dev" } };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "agent" && id === "agent-1"
        ? "Runtime Local Skills OpenClaw Demo"
        : "dev",
    getActorInitials: (type: string, id: string) =>
      type === "agent" && id === "agent-1" ? "RL" : "D",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspaces"],
    queryFn: () =>
      Promise.resolve([
        {
          id: "ws-1",
          name: "Runtime Local Skills Demo",
          slug: "runtime-local-skills-demo",
          created_at: "2026-04-23T00:00:00Z",
          updated_at: "2026-04-23T00:00:00Z",
        },
      ]),
  }),
  memberListOptions: () => ({
    queryKey: ["members", "ws-1"],
    queryFn: () =>
      Promise.resolve([
        {
          id: "member-1",
          user_id: "user-1",
          name: "dev",
          email: "dev@localhost",
          role: "owner",
        },
      ]),
  }),
  agentListOptions: () => ({
    queryKey: ["agents", "ws-1"],
    queryFn: () =>
      Promise.resolve([
        {
          id: "agent-1",
          workspace_id: "ws-1",
          runtime_id: "runtime-1",
          name: "Runtime Local Skills OpenClaw Demo",
          archived_at: null,
          visibility: "workspace",
          owner_id: null,
        },
      ]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["assignee-frequency", "ws-1"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector?: any) =>
    selector ? selector(selectionState) : selectionState,
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({
    mutateAsync: (...args: unknown[]) => mockBatchUpdateIssues(...args),
    isPending: false,
  }),
  useBatchDeleteIssues: () => ({
    mutateAsync: (...args: unknown[]) => mockBatchDeleteIssues(...args),
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { BatchActionToolbar } from "./batch-action-toolbar";

const issueDefaults = {
  workspace_id: "ws-1",
  number: 1,
  identifier: "RUN-1",
  title: "Test issue",
  description: null,
  status: "todo" as const,
  priority: "none" as const,
  creator_type: "member" as const,
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  due_date: null,
  created_at: "2026-04-23T00:00:00Z",
  updated_at: "2026-04-23T00:00:00Z",
};

function makeIssue(overrides: Partial<Issue>): Issue {
  return {
    ...issueDefaults,
    id: overrides.id ?? "issue-1",
    assignee_type: null,
    assignee_id: null,
    ...overrides,
  };
}

function renderToolbar(issues: Issue[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <BatchActionToolbar issues={issues} />
    </QueryClientProvider>,
  );
}

describe("BatchActionToolbar", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    selectionState.selectedIds = new Set(["issue-1", "issue-2"]);
  });

  afterEach(() => {
    cleanup();
  });

  it("marks the shared selected assignee instead of defaulting to unassigned", async () => {
    renderToolbar([
      makeIssue({ id: "issue-1", assignee_type: "agent", assignee_id: "agent-1" }),
      makeIssue({ id: "issue-2", assignee_type: "agent", assignee_id: "agent-1" }),
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Assignee" }));

    const agentOption = await screen.findByText("Runtime Local Skills OpenClaw Demo");
    const agentButton = agentOption.closest("button");
    const unassignedButton = screen.getByText("Unassigned").closest("button");

    expect(agentButton).toHaveAttribute("data-selected", "true");
    expect(unassignedButton).not.toHaveAttribute("data-selected");
  });

  it("does not mark unassigned when selected issues have mixed assignees", async () => {
    renderToolbar([
      makeIssue({ id: "issue-1", assignee_type: "agent", assignee_id: "agent-1" }),
      makeIssue({ id: "issue-2", assignee_type: null, assignee_id: null }),
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Assignee" }));

    await waitFor(() => {
      expect(screen.getByText("Runtime Local Skills OpenClaw Demo")).toBeVisible();
    });
    expect(screen.getByText("Unassigned").closest("button")).not.toHaveAttribute(
      "data-selected",
    );
    expect(
      screen.getByText("Runtime Local Skills OpenClaw Demo").closest("button"),
    ).not.toHaveAttribute("data-selected");
  });
});
