import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockOpenModal = vi.hoisted(() => vi.fn());
const mockViewState = vi.hoisted(() => ({
  sortBy: "position",
  sortDirection: "asc",
  listCollapsedStatuses: [] as string[],
  toggleListCollapsed: vi.fn(),
  hideStatus: vi.fn(),
  showStatus: vi.fn(),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    () => ({ open: mockOpenModal }),
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

vi.mock("@multica/core/issues/config", () => ({
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
  STATUS_CONFIG: {
    todo: {
      label: "Todo",
      badgeBg: "bg-muted",
      badgeText: "text-muted-foreground",
      columnBg: "bg-muted/40",
    },
  },
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useLoadMoreByStatus: () => ({
    loadMore: vi.fn(),
    hasMore: false,
    isLoading: false,
    total: 0,
  }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector?: any) => (selector ? selector(mockViewState) : mockViewState),
  useViewStoreApi: () => ({ getState: () => mockViewState }),
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: Object.assign(
    (selector?: any) => {
      const state = {
        selectedIds: new Set<string>(),
        select: vi.fn(),
        deselect: vi.fn(),
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        selectedIds: new Set<string>(),
        select: vi.fn(),
        deselect: vi.fn(),
      }),
    },
  ),
}));

vi.mock("@dnd-kit/core", () => ({
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => children,
  verticalListSortingStrategy: {},
}));

vi.mock("@base-ui/react/accordion", () => ({
  Accordion: {
    Root: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    Item: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    Header: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    Trigger: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
    Panel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
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

vi.mock("./status-icon", () => ({
  StatusIcon: () => <span data-testid="status-icon" />,
}));

vi.mock("./board-card", () => ({
  DraggableBoardCard: ({ issue }: any) => <div>{issue.title}</div>,
  BoardCardContent: ({ issue }: any) => <div>{issue.title}</div>,
}));

vi.mock("./list-row", () => ({
  ListRow: ({ issue }: any) => <div>{issue.title}</div>,
}));

vi.mock("./infinite-scroll-sentinel", () => ({
  InfiniteScrollSentinel: () => null,
}));

import { BoardColumn } from "./board-column";
import { ListView } from "./list-view";

describe("project issue create prefill", () => {
  beforeEach(() => {
    mockOpenModal.mockClear();
  });

  it("passes the current project to the board column create button", async () => {
    render(
      <BoardColumn
        status="todo"
        issueIds={[]}
        issueMap={new Map()}
        projectId="project-1"
      />,
    );

    await userEvent.click(screen.getByLabelText("Add issue"));

    expect(mockOpenModal).toHaveBeenCalledWith("create-issue", {
      status: "todo",
      project_id: "project-1",
    });
  });

  it("passes the current project to the list section create button", async () => {
    render(
      <ListView
        issues={[]}
        visibleStatuses={["todo"]}
        projectId="project-1"
      />,
    );

    await userEvent.click(screen.getByLabelText("Add issue"));

    expect(mockOpenModal).toHaveBeenCalledWith("create-issue", {
      status: "todo",
      project_id: "project-1",
    });
  });
});
