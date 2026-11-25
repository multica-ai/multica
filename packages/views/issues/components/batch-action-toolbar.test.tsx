import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { Issue } from "@multica/core/types";
import { BatchActionToolbar } from "./batch-action-toolbar";

// Mutable selection state shared with the store mock below. The real toolbar
// derives the pickers' current value from the issues it can resolve out of
// this selection, so each test sets `selection.selectedIds` before rendering.
const selection = vi.hoisted(() => ({
  selectedIds: new Set<string>(),
  clear: () => {},
}));

const deletionPolicy = vi.hoisted(() => ({ allowed: true }));
vi.mock("@multica/core/issues/use-issue-deletion", () => ({
  useIssueDeletion: () => ({ canDelete: () => deletionPolicy.allowed, offersDelete: true }),
}));

const viewer = vi.hoisted(() => ({ guest: false }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => viewer.guest,
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ getShareableUrl: (path: string) => `https://app.example${path}` }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: (selector: (s: typeof selection) => unknown) =>
    selector(selection),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

const { WORKFLOWS, PROJECTS } = vi.hoisted(() => {
  const workflow = (id: string, isDefault: boolean, keys: string[]) => ({
    id,
    workspace_id: "ws-1",
    name: id,
    description: "",
    is_default: isDefault,
    steps: keys.map((status_key) => ({ status_key, handler: { type: "none" }, instructions: "" })),
    project_ids: [],
    created_at: "",
    updated_at: "",
  });
  return {
    WORKFLOWS: [
      workflow("default", true, ["todo", "in_progress", "done"]),
      workflow("dev", false, ["todo", "in_progress", "code_review", "done"]),
      workflow("design", false, ["todo", "mockup", "done"]),
    ],
    PROJECTS: [
      { id: "p-dev", workflow_id: "dev" },
      { id: "p-design", workflow_id: "design" },
      { id: "p-default", workflow_id: null },
    ],
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issue-workflows", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-workflows")>()),
  useIssueWorkflows: () => ({ workflows: WORKFLOWS }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: PROJECTS }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "label" }),
}));

// Render each picker as a probe that surfaces the value the toolbar passed in,
// so the test asserts the wiring (real `commonIssueFields` runs underneath).
vi.mock("./pickers", () => ({
  StatusPicker: ({ status, allowedStatuses }: { status: string | null; allowedStatuses?: ReadonlySet<string> | null }) => (
    <div
      data-testid="status-picker"
      data-status={status ?? "__none__"}
      data-allowed={allowedStatuses ? [...allowedStatuses].sort().join(",") : "__all__"}
    />
  ),
  PriorityPicker: ({ priority }: { priority: string | null }) => (
    <div data-testid="priority-picker" data-priority={priority ?? "__none__"} />
  ),
  AssigneePicker: ({
    assigneeType,
    assigneeId,
    mixed,
  }: {
    assigneeType: string | null;
    assigneeId: string | null;
    mixed?: boolean;
  }) => (
    <div
      data-testid="assignee-picker"
      data-assignee-type={assigneeType ?? "__null__"}
      data-assignee-id={assigneeId ?? "__null__"}
      data-mixed={String(Boolean(mixed))}
    />
  ),
  DueDatePicker: ({ dueDate }: { dueDate: string | null }) => (
    <div data-testid="due-date-picker" data-due={dueDate ?? "__none__"} />
  ),
  StartDatePicker: () => null,
}));
vi.mock("./pickers/batch-label-picker", () => ({
  BatchLabelPicker: ({ issues }: { issues: Issue[] }) => (
    <div data-testid="label-picker" data-issues={issues.map((issue) => issue.id).join(",")} />
  ),
}));
vi.mock("../../projects/components/project-picker", () => ({
  ProjectPicker: ({ projectId }: { projectId: string | null | undefined }) => (
    <div data-testid="project-picker" data-project={projectId === undefined ? "__mixed__" : projectId ?? "__none__"} />
  ),
}));

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Issue 1",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  deletionPolicy.allowed = true;
  viewer.guest = false;
  selection.selectedIds = new Set();
});

describe("BatchActionToolbar for a guest (MUL-9883)", () => {
  it("offers only Copy links in place of the field pickers and the More menu", () => {
    viewer.guest = true;
    const issues = [makeIssue({ id: "a" }), makeIssue({ id: "b" })];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    for (const picker of ["status-picker", "priority-picker", "assignee-picker", "label-picker", "project-picker", "due-date-picker"]) {
      expect(screen.queryByTestId(picker)).not.toBeInTheDocument();
    }
    // No More menu: its trigger is the bar's only menu button.
    expect(document.querySelector('button[aria-haspopup="menu"]')).toBeNull();
    // Clear selection, then Copy links.
    expect(screen.getAllByRole("button")).toHaveLength(2);
  });
});

describe("BatchActionToolbar picker wiring", () => {
  it("reflects the shared status / priority / assignee of the selected issues", () => {
    const issues = [
      makeIssue({ id: "a", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "in_progress");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "high");
    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-assignee-type", "member");
    expect(assignee).toHaveAttribute("data-assignee-id", "u-1");
    expect(assignee).toHaveAttribute("data-mixed", "false");
  });

  it("falls back to an empty (no-checkmark) state when the selection is mixed", () => {
    const issues = [
      makeIssue({ id: "a", status: "todo", priority: "none", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "done", priority: "urgent", assignee_type: "agent", assignee_id: "ag-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "__none__");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "__none__");
    expect(screen.getByTestId("assignee-picker")).toHaveAttribute("data-mixed", "true");
  });

  it("treats an all-unassigned selection as unassigned, not mixed", () => {
    const issues = [
      makeIssue({ id: "a", assignee_type: null, assignee_id: null }),
      makeIssue({ id: "b", assignee_type: null, assignee_id: null }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-mixed", "false");
    expect(assignee).toHaveAttribute("data-assignee-type", "__null__");
    expect(assignee).toHaveAttribute("data-assignee-id", "__null__");
  });

  it("hands the label, project and due date pickers the selection's shared values", () => {
    const issues = [
      makeIssue({ id: "a", project_id: "p-dev", due_date: "2026-02-01" }),
      makeIssue({ id: "b", project_id: "p-dev", due_date: "2026-02-01" }),
      makeIssue({ id: "c", project_id: null, due_date: "2026-03-01" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);
    const view = render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("label-picker")).toHaveAttribute("data-issues", "a,b");
    expect(screen.getByTestId("project-picker")).toHaveAttribute("data-project", "p-dev");
    expect(screen.getByTestId("due-date-picker")).toHaveAttribute("data-due", "2026-02-01");

    selection.selectedIds = new Set(["a", "b", "c"]);
    view.rerender(<BatchActionToolbar issues={[...issues]} />);
    expect(screen.getByTestId("project-picker")).toHaveAttribute("data-project", "__mixed__");
    expect(screen.getByTestId("due-date-picker")).toHaveAttribute("data-due", "__none__");
  });

  it("offers only the statuses every selected project's workflow allows", () => {
    const issues = [
      makeIssue({ id: "a", project_id: "p-dev" }),
      makeIssue({ id: "b", project_id: "p-design" }),
      makeIssue({ id: "c", project_id: "p-default" }),
    ];
    selection.selectedIds = new Set(["a", "b", "c"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-allowed", "done,todo");
  });

  it("offers Default's statuses for issues on Default or outside any project (MUL-8250)", () => {
    const issues = [makeIssue({ id: "a", project_id: "p-default" }), makeIssue({ id: "b" })];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-allowed", "done,in_progress,todo");
  });

  it("renders nothing when nothing is selected", () => {
    render(<BatchActionToolbar issues={[makeIssue({ id: "a" })]} />);
    expect(screen.queryByTestId("status-picker")).toBeNull();
  });

  it("removes the toolbar after the final selected issue is cleared", async () => {
    const issues = [makeIssue({ id: "a" })];
    selection.selectedIds = new Set(["a"]);
    const view = render(<BatchActionToolbar issues={issues} />);

    expect(screen.getByTestId("status-picker")).toBeInTheDocument();
    selection.selectedIds = new Set();
    view.rerender(<BatchActionToolbar issues={issues} />);

    await waitFor(() => {
      expect(screen.queryByTestId("status-picker")).not.toBeInTheDocument();
    });
  });
});
