import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, within, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { BoardView } from "./board-view";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// PRIORITY_ORDER drives sortIssues' "priority" ranking.
vi.mock("@multica/core/issues/config", () => ({
  PRIORITY_ORDER: ["urgent", "high", "medium", "low", "none"],
}));

// MUST be a stable reference: production `useActorName` memoizes its return,
// and board-view feeds `getActorName` into a `useMemo` that drives the
// columns-rebuild effect. A fresh object per render loops it forever.
const { mockActorName } = vi.hoisted(() => ({
  mockActorName: {
    getActorName: () => "Mock Actor",
    getActorInitials: () => "MA",
    getActorAvatarUrl: () => null,
  },
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => mockActorName,
}));

// View store — mutable so each test sets the sort it needs.
const mockViewState = {
  grouping: "status" as "status" | "assignee",
  sortBy: "position" as
    | "position"
    | "priority"
    | "start_date"
    | "due_date"
    | "created_at"
    | "title",
  sortDirection: "asc" as "asc" | "desc",
};
vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  ViewStoreProvider: ({ children }: { children: React.ReactNode }) => children,
  useViewStore: (selector?: (s: typeof mockViewState) => unknown) =>
    selector ? selector(mockViewState) : mockViewState,
  useViewStoreApi: () => ({
    getState: () => mockViewState,
    setState: vi.fn(),
    subscribe: vi.fn(),
  }),
}));

const useLoadMoreByStatusMock = vi.fn(() => ({
  total: 0,
  loaded: 0,
  hasMore: false,
  isLoading: false,
  loadMore: vi.fn(),
}));
vi.mock("@multica/core/issues/mutations", () => ({
  useLoadMoreByStatus: () => useLoadMoreByStatusMock(),
  useLoadMoreByAssigneeGroup: () => useLoadMoreByStatusMock(),
}));

// Capture the drag handlers so tests can drive drag-over / drag-end directly.
let lastOnDragEnd: ((event: unknown) => void) | null = null;
let lastOnDragOver: ((event: unknown) => void) | null = null;
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({
    children,
    onDragEnd,
    onDragOver,
  }: {
    children: React.ReactNode;
    onDragEnd: (event: unknown) => void;
    onDragOver: (event: unknown) => void;
  }) => {
    lastOnDragEnd = onDragEnd;
    lastOnDragOver = onDragOver;
    return children;
  },
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  pointerWithin: vi.fn(),
  closestCenter: vi.fn(),
}));

// Replace heavy column/card rendering with a thin probe that exposes the
// per-column ordered issue ids — exactly what BoardView is responsible for.
vi.mock("./board-column", () => ({
  BOARD_CARD_WIDTH: 200,
  BoardColumn: ({
    group,
    issueIds,
  }: {
    group: { id: string };
    issueIds: string[];
  }) => (
    <div data-testid={`col-${group.id}`}>
      {issueIds.map((id) => (
        <span key={id} data-testid="card">
          {id}
        </span>
      ))}
    </div>
  ),
}));
vi.mock("./board-card", () => ({ BoardCardContent: () => null }));
vi.mock("./hidden-columns-panel", () => ({
  HiddenColumnsPanel: () => null,
  HiddenColumnRow: () => null,
}));
vi.mock("./infinite-scroll-sentinel", () => ({
  InfiniteScrollSentinel: () => null,
}));

function makeIssue(over: Partial<Issue> & { id: string }): Issue {
  return {
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: over.id,
    description: "",
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

function renderBoard(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        {ui}
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const VISIBLE = ["todo", "in_progress"] as const;

describe("BoardView non-manual ordering", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockViewState.grouping = "status";
    mockViewState.sortBy = "position";
    mockViewState.sortDirection = "asc";
    lastOnDragEnd = null;
    lastOnDragOver = null;
  });

  it("orders cards within a column by the active non-position sort", () => {
    mockViewState.sortBy = "priority";
    // Out of priority order on purpose; cache/array order is low, urgent, medium.
    const issues = [
      makeIssue({ id: "low", status: "todo", priority: "low" }),
      makeIssue({ id: "urgent", status: "todo", priority: "urgent" }),
      makeIssue({ id: "medium", status: "todo", priority: "medium" }),
    ];

    const { getByTestId } = renderBoard(
      <BoardView
        issues={issues}
        visibleStatuses={[...VISIBLE]}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
      />,
    );

    const cards = within(getByTestId("col-status:todo")).getAllByTestId("card");
    expect(cards.map((c) => c.textContent)).toEqual(["urgent", "medium", "low"]);
  });

  it("places an optimistically-moved card into its sorted slot without waiting for settle", () => {
    mockViewState.sortBy = "priority";
    const onMoveIssue = vi.fn();
    const mover = makeIssue({ id: "mover", status: "todo", priority: "high" });
    const initial = [
      mover,
      makeIssue({ id: "stay-urgent", status: "in_progress", priority: "urgent" }),
      makeIssue({ id: "stay-low", status: "in_progress", priority: "low" }),
    ];

    const { getByTestId, rerender } = renderBoard(
      <BoardView
        issues={initial}
        visibleStatuses={[...VISIBLE]}
        hiddenStatuses={[]}
        onMoveIssue={onMoveIssue}
      />,
    );

    // Drag "mover" from todo into the in_progress column.
    act(() => {
      lastOnDragEnd?.({
        active: { id: "mover" },
        over: { id: "status:in_progress" },
      });
    });

    // The move is requested with no settle callback — the gate is gone.
    expect(onMoveIssue).toHaveBeenCalledTimes(1);
    const [movedId, updates, settleCb] = onMoveIssue.mock.calls[0]!;
    expect(movedId).toBe("mover");
    expect(updates).toMatchObject({ status: "in_progress" });
    expect(settleCb).toBeUndefined();

    // Simulate the optimistic cache patch (status flipped) WITHOUT firing any
    // settle callback. The card must already sit in its sorted slot — between
    // urgent and low — in the in_progress column.
    const patched = [
      makeIssue({ id: "mover", status: "in_progress", priority: "high" }),
      makeIssue({ id: "stay-urgent", status: "in_progress", priority: "urgent" }),
      makeIssue({ id: "stay-low", status: "in_progress", priority: "low" }),
    ];
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <I18nProvider resources={TEST_RESOURCES} locale="en">
          <BoardView
            issues={patched}
            visibleStatuses={[...VISIBLE]}
            hiddenStatuses={[]}
            onMoveIssue={onMoveIssue}
          />
        </I18nProvider>
      </QueryClientProvider>,
    );

    const todo = within(getByTestId("col-status:todo")).queryAllByTestId("card");
    expect(todo.map((c) => c.textContent)).toEqual([]);
    const inProgress = within(getByTestId("col-status:in_progress")).getAllByTestId(
      "card",
    );
    expect(inProgress.map((c) => c.textContent)).toEqual([
      "stay-urgent",
      "mover",
      "stay-low",
    ]);
  });

  it("previews a non-manual cross-column drag in the target's sorted slot (no drop)", () => {
    // The smoothness fix: during the drag (onDragOver, before any drop) the card
    // must already sit in its sorted slot in the target column. Without this it
    // stays glued to the source column and teleports on release.
    mockViewState.sortBy = "priority";
    const mover = makeIssue({ id: "mover", status: "todo", priority: "high" });
    const issues = [
      mover,
      makeIssue({ id: "stay-urgent", status: "in_progress", priority: "urgent" }),
      makeIssue({ id: "stay-low", status: "in_progress", priority: "low" }),
    ];

    const { getByTestId } = renderBoard(
      <BoardView
        issues={issues}
        visibleStatuses={[...VISIBLE]}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
      />,
    );

    // Drag (not drop) "mover" over the in_progress column.
    act(() => {
      lastOnDragOver?.({
        active: { id: "mover" },
        over: { id: "status:in_progress" },
      });
    });

    const todo = within(getByTestId("col-status:todo")).queryAllByTestId("card");
    expect(todo.map((c) => c.textContent)).toEqual([]);
    const inProgress = within(
      getByTestId("col-status:in_progress"),
    ).getAllByTestId("card");
    // Sorted by priority: urgent, then the dragged high, then low.
    expect(inProgress.map((c) => c.textContent)).toEqual([
      "stay-urgent",
      "mover",
      "stay-low",
    ]);
  });

  it("keeps Manual (position) order ascending even with a stale desc direction", () => {
    // The header hides the direction toggle in manual mode but never resets a
    // desc left over from a prior field-sort. Manual order must stay position
    // ascending regardless — the server treats position as directionless.
    mockViewState.sortBy = "position";
    mockViewState.sortDirection = "desc";
    const issues = [
      makeIssue({ id: "p30", status: "todo", position: 30 }),
      makeIssue({ id: "p10", status: "todo", position: 10 }),
      makeIssue({ id: "p20", status: "todo", position: 20 }),
    ];

    const { getByTestId } = renderBoard(
      <BoardView
        issues={issues}
        visibleStatuses={[...VISIBLE]}
        hiddenStatuses={[]}
        onMoveIssue={vi.fn()}
      />,
    );

    const cards = within(getByTestId("col-status:todo")).getAllByTestId("card");
    expect(cards.map((c) => c.textContent)).toEqual(["p10", "p20", "p30"]);
  });
});
