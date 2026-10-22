import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { BUILT_IN_STATUS_ORDER } from "@multica/core/issues/config";
import { BoardColumn } from "./board-column";
import { ListView } from "./list-view";
import type { IssueStatusPagination } from "../surface/use-issue-status-branches";

vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([{ key: "retired", name: "Retired", category: "started", is_system: false, archived_at: "2026-01-01" } as any]) };
});

const openModal = vi.hoisted(() => vi.fn());
const hideStatus = vi.hoisted(() => vi.fn());
const showStatus = vi.hoisted(() => vi.fn());
const select = vi.hoisted(() => vi.fn());
const deselect = vi.hoisted(() => vi.fn());

function emptyStatusPagination(): IssueStatusPagination {
  return Object.fromEntries(
    BUILT_IN_STATUS_ORDER.map((status) => [
      status,
      {
        total: 0,
        loaded: 0,
        hasMore: false,
        isLoading: false,
        isFetching: false,
        isError: false,
        loadMore: vi.fn(),
        retry: vi.fn(),
      },
    ]),
  ) as unknown as IssueStatusPagination;
}

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector?: any) => {
    const state = {
      grouping: "status",
      sortBy: "position",
      listCollapsedStatuses: [],
      toggleListCollapsed: vi.fn(),
    };
    return selector ? selector(state) : state;
  },
  useViewStoreApi: () => ({
    getState: () => ({ hideStatus, showStatus }),
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => id,
  }),
}));

vi.mock("../surface/selection-context", () => ({
  useIssueSurfaceSelection: () => ({
    selectedIds: new Set<string>(),
    select,
    deselect,
    toggle: vi.fn(),
    clear: vi.fn(),
  }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "translated" }),
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => children,
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: () => ({ setNodeRef: vi.fn(), isOver: false }),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => children,
  verticalListSortingStrategy: {},
  arrayMove: <T,>(items: T[]) => items,
}));

vi.mock("@base-ui/react/accordion", () => ({
  Accordion: {
    Root: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    Item: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
    Header: ({ children, className }: { children: React.ReactNode; className?: string }) => (
      <div className={className}>{children}</div>
    ),
    Trigger: ({ children, className }: { children: React.ReactNode; className?: string }) => (
      <button type="button" className={className}>
        {children}
      </button>
    ),
    Panel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  },
}));

beforeEach(() => {
  openModal.mockClear();
  hideStatus.mockClear();
  showStatus.mockClear();
  select.mockClear();
  deselect.mockClear();
});

describe("issue renderer create entrypoints", () => {
  it("does not offer creation into an archived board/list status", () => {
    const onCreateIssue = vi.fn();
    const { container } = render(<>
      <BoardColumn group={{ id: "status:retired", title: "Retired", status: "retired" }}
        issueIds={[]} issueMap={new Map()} onCreateIssue={onCreateIssue} />
      <ListView issues={[]} visibleStatuses={["retired"]} statusPagination={emptyStatusPagination()}
        onCreateIssue={onCreateIssue} />
    </>);
    expect(container.querySelector(".lucide-plus")).toBeNull();
    expect(onCreateIssue).not.toHaveBeenCalled();
  });

  it.each(["todo", "in_review", "awaiting_response"])("creates in the exact board column %s", (status) => {
    const onCreateIssue = vi.fn();

    render(
      <BoardColumn
        group={{
          id: status,
          title: status,
          status,
          createData: { status },
        }}
        issueIds={[]}
        issueMap={new Map()}
        projectId="project-1"
        onCreateIssue={onCreateIssue}
      />,
    );

    const buttons = screen.getAllByRole("button");
    fireEvent.click(buttons[buttons.length - 1]!);

    expect(onCreateIssue).toHaveBeenCalledWith({
      status,
      project_id: "project-1",
    });
    expect(openModal).not.toHaveBeenCalled();
  });

  it.each(["todo", "in_review", "awaiting_response"])("creates in the exact list section %s", (status) => {
    const onCreateIssue = vi.fn();

    render(
      <ListView
        issues={[]}
        visibleStatuses={[status]}
        statusPagination={emptyStatusPagination()}
        projectId="project-1"
        onCreateIssue={onCreateIssue}
      />,
    );

    const buttons = screen.getAllByRole("button");
    fireEvent.click(buttons[buttons.length - 1]!);

    expect(onCreateIssue).toHaveBeenCalledWith({
      status,
      project_id: "project-1",
    });
    expect(openModal).not.toHaveBeenCalled();
  });
});
