/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../actions/issue-actions-context-menu";
import { BoardView } from "./board-view";

import type { DragEndEvent } from "@dnd-kit/core";
import type { ReactNode } from "react";

let lastOnDragEnd: ((event: DragEndEvent) => void) | null = null;

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({
    children,
    onDragEnd,
  }: {
    children: ReactNode;
    onDragEnd?: (event: DragEndEvent) => void;
  }) => {
    lastOnDragEnd = onDragEnd ?? null;
    return children;
  },
  DragOverlay: () => null,
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  useDroppable: ({ id, data }: { id: string; data?: unknown }) => ({
    setNodeRef: vi.fn(),
    isOver: false,
    id,
    data,
  }),
  pointerWithin: vi.fn(),
  closestCenter: vi.fn(),
}));

function triggerDragEnd(activeId: string, overId: string) {
  lastOnDragEnd?.({
    active: { id: activeId },
    over: { id: overId },
  } as unknown as DragEndEvent);
}

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: any) => children,
  verticalListSortingStrategy: {},
  arrayMove: <T,>(arr: T[], from: number, to: number): T[] => {
    const copy = arr.slice();
    const [item] = copy.splice(from, 1);
    copy.splice(to, 0, item!);
    return copy;
  },
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/properties", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/properties")>()),
  propertyListOptions: () => ({
    queryKey: ["properties"],
    queryFn: async () => [],
  }),
  useSetIssueProperty: () => ({ mutate: () => {} }),
  useUnsetIssueProperty: () => ({ mutate: () => {} }),
}));

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: () => "Someone" }),
}));

vi.mock("@multica/core/auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/auth")>()),
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "viewer-1" } }),
}));

vi.mock("@multica/core/agents", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents")>()),
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => ({ availability: "offline", workload: null }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => (
    <a {...props}>{children}</a>
  ),
  useNavigation: () => ({
    push: () => {},
    openInNewTab: () => {},
    getShareableUrl: (path: string) => `https://app.example${path}`,
    pathname: "/",
  }),
  resolveClickIntent: () => "push",
  useIntentNavigate: () => () => {},
}));

function makeIssue(id: string, status: Issue["status"], position = 100): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Issue ${id}`,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

describe("BoardView drag into hidden status columns", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    lastOnDragEnd = null;
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders hidden columns panel and calls onMoveIssue when card is dropped on a hidden status", () => {
    const mockOnMoveIssue = vi.fn();
    const issue1 = makeIssue("issue-1", "todo", 250);
    const store = getIssueSurfaceViewStore(
      `board-hidden-${Math.floor(Math.random() * 1e9)}`,
    );
    store.getState().setGrouping("status");

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueContextMenuProvider>
            <BoardView
              issues={[issue1]}
              visibleStatuses={["backlog", "todo", "in_progress"]}
              hiddenStatuses={["done", "cancelled"]}
              onMoveIssue={mockOnMoveIssue}
            />
          </IssueContextMenuProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );

    expect(screen.getByText("Hidden columns")).toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
    expect(screen.getByText("Cancelled")).toBeInTheDocument();

    // Simulate drag and drop onto hidden column "Done" (id: "status:done")
    act(() => {
      triggerDragEnd("issue-1", "status:done");
    });

    expect(mockOnMoveIssue).toHaveBeenCalledTimes(1);
    expect(mockOnMoveIssue).toHaveBeenCalledWith(
      "issue-1",
      {
        status: "done",
        position: 250,
        before_id: null,
        after_id: null,
      },
      expect.any(Function),
    );
  });

  it("does not call onMoveIssue when card is already in the target hidden status", () => {
    const mockOnMoveIssue = vi.fn();
    const doneIssue = makeIssue("issue-done", "done", 100);
    const store = getIssueSurfaceViewStore(
      `board-hidden-${Math.floor(Math.random() * 1e9)}`,
    );
    store.getState().setGrouping("status");

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueContextMenuProvider>
            <BoardView
              issues={[doneIssue]}
              visibleStatuses={["todo", "in_progress"]}
              hiddenStatuses={["done"]}
              onMoveIssue={mockOnMoveIssue}
            />
          </IssueContextMenuProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );

    act(() => {
      triggerDragEnd("issue-done", "status:done");
    });

    expect(mockOnMoveIssue).not.toHaveBeenCalled();
  });
});
