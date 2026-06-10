import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Issue } from "@multica/core/types";

const viewState = vi.hoisted(() => ({
  sortBy: "position",
  sortDirection: "asc",
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
}));

vi.mock("@multica/core/issues/config", () => ({
  ALL_STATUSES: ["todo", "done"],
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  STATUS_CONFIG: {
    todo: { columnBg: "bg-muted/40", iconColor: "text-muted-foreground" },
    done: { columnBg: "bg-info/5", iconColor: "text-info" },
  },
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useLoadMoreByStatus: (status: string) => ({
    loadMore: vi.fn(),
    hasMore: false,
    isLoading: false,
    total: status === "todo" ? 2 : 27,
  }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector?: any) => (selector ? selector(viewState) : viewState),
  useViewStoreApi: () => ({ getState: () => viewState }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: any) =>
      selector({
        status: { todo: "Todo", done: "Done" },
        board: {
          add_issue_tooltip: "Add issue",
          empty_column: "No matching issues",
          hidden_columns_label: "Hidden columns",
          hide_column: "Hide column",
          show_column: "Show column",
        },
      }),
  }),
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DragOverlay: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
  pointerWithin: () => [],
  closestCenter: () => [],
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  verticalListSortingStrategy: {},
  arrayMove: (items: string[], from: number, to: number) => {
    const next = [...items];
    const [item] = next.splice(from, 1);
    if (item) next.splice(to, 0, item);
    return next;
  },
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render?: React.ReactNode }) => render,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render?: React.ReactNode }) => render,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("./board-card", () => ({
  DraggableBoardCard: ({ issue }: { issue: Issue }) => <div>{issue.title}</div>,
  BoardCardContent: ({ issue }: { issue: Issue }) => <div>{issue.title}</div>,
}));

vi.mock("./infinite-scroll-sentinel", () => ({
  InfiniteScrollSentinel: () => null,
}));

import { BoardView } from "./board-view";

function issue({
  id,
  status,
  title,
  ...overrides
}: Partial<Issue> & Pick<Issue, "id" | "status" | "title">): Issue {
  return {
    id,
    workspace_id: "ws-test",
    number: 1,
    identifier: id.toUpperCase(),
    title,
    description: null,
    status,
    priority: "high",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "creator-1",
    parent_issue_id: null,
    project_id: "project-1",
    position: 0,
    due_date: null,
    start_date: null,
    labels: [],
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

describe("BoardView column counts", () => {
  it("uses filtered column count overrides for status headings and empty columns", () => {
    render(
      <BoardView
        issues={[
          issue({
            id: "issue-1",
            title: "High priority todo",
            status: "todo",
          }),
        ]}
        visibleStatuses={["todo", "done"]}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
        columnCounts={{ todo: 1, done: 0 }}
      />,
    );

    expect(screen.getByText("Todo")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
    expect(screen.getByText("0")).toBeInTheDocument();
    expect(screen.getByText("No matching issues")).toBeInTheDocument();
    expect(screen.queryByText("27")).not.toBeInTheDocument();
  });

  it("falls back to server totals when count overrides are absent", () => {
    render(
      <BoardView
        issues={[
          issue({
            id: "issue-1",
            title: "High priority todo",
            status: "todo",
          }),
        ]}
        visibleStatuses={["todo", "done"]}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
      />,
    );

    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("27")).toBeInTheDocument();
  });
});
