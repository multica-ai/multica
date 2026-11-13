/**
 * @vitest-environment jsdom
 *
 * Board sub-issue nesting, drag side: moving a card to another column takes
 * the sub-issues shown inside it along, and nothing else.
 */
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { act, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type { Issue, IssueStatus } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../actions/issue-actions-context-menu";
import {
  IssueSurfaceActionsProvider,
  type IssueSurfaceActions,
} from "../surface/actions-context";
import type { DragMoveUpdates } from "../utils/drag-utils";
import { BoardView } from "./board-view";

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
  const actual =
    await importOriginal<typeof import("@multica/core/paths")>();
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

// Capture the board's drag handlers so a drop can be driven directly; pointer
// geometry is not what these tests are about.
const dnd = vi.hoisted(() => ({ handlers: {} as Record<string, (event: unknown) => void> }));
vi.mock("@dnd-kit/core", () => {
  const stableSetNodeRef = () => {};
  return {
    DndContext: ({ children, ...handlers }: Record<string, unknown>) => {
      dnd.handlers = handlers as Record<string, (event: unknown) => void>;
      return children;
    },
    DragOverlay: () => null,
    PointerSensor: class {},
    useSensor: () => ({}),
    useSensors: () => [],
    useDroppable: () => ({ setNodeRef: stableSetNodeRef, isOver: false }),
    pointerWithin: vi.fn(() => []),
    closestCenter: vi.fn(() => []),
  };
});

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => children,
  verticalListSortingStrategy: {},
  arrayMove: <T,>(items: T[]) => items,
  defaultAnimateLayoutChanges: () => false,
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: () => {},
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

function makeIssue(
  id: string,
  status: IssueStatus,
  parentId: string | null = null,
): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Task ${id}`,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: parentId,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const ISSUES = [
  makeIssue("parent", "todo"),
  makeIssue("child", "todo", "parent"),
  makeIssue("grandchild", "todo", "child"),
  makeIssue("other", "todo"),
  makeIssue("finished", "done", "parent"),
];

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

describe("Board drag with nested sub-issues", () => {
  let queryClient: QueryClient;
  let onMoveIssue: Mock<
    (issueId: string, updates: DragMoveUpdates, onSettled?: () => void) => void
  >;
  let batchUpdate: Mock<IssueSurfaceActions["batchUpdate"]>;

  beforeEach(() => {
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    onMoveIssue = vi.fn();
    batchUpdate = vi.fn<IssueSurfaceActions["batchUpdate"]>((ids) =>
      Promise.resolve({ updated: ids.length }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  function render({ hierarchy = true } = {}) {
    const store = getIssueSurfaceViewStore(
      `board-carry-${Math.floor(Math.random() * 1e9)}`,
    );
    if (!hierarchy) store.getState().toggleBoardHierarchy();
    const actions = {
      isPending: false,
      createIssue: vi.fn(),
      updateIssue: vi.fn(),
      moveIssue: vi.fn(),
      batchUpdate,
      batchDelete: vi.fn(),
    };
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueSurfaceActionsProvider actions={actions}>
            <IssueContextMenuProvider>
              <BoardView
                issues={ISSUES}
                visibleStatuses={["todo", "done"]}
                onMoveIssue={onMoveIssue}
              />
            </IssueContextMenuProvider>
          </IssueSurfaceActionsProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );
  }

  function drop(issueId: string, columnId: string) {
    act(() => dnd.handlers.onDragStart!({ active: { id: issueId } }));
    act(() =>
      dnd.handlers.onDragEnd!({ active: { id: issueId }, over: { id: columnId } }),
    );
  }

  it("moves the sub-issues inside a card along with it", () => {
    render();

    drop("parent", "status:done");

    expect(onMoveIssue).toHaveBeenCalledWith(
      "parent",
      expect.objectContaining({ status: "done" }),
      expect.any(Function),
    );
    // "finished" already sits in Done, so only the card's contents move.
    expect(batchUpdate).toHaveBeenCalledWith(["child", "grandchild"], { status: "done" });
  });

  it("takes only the dragged card's own sub-issues, never its parent", () => {
    render();

    drop("child", "status:done");

    expect(batchUpdate).toHaveBeenCalledWith(["grandchild"], { status: "done" });
  });

  it("moves no sub-issues when the card has none", () => {
    render();

    drop("other", "status:done");

    expect(onMoveIssue).toHaveBeenCalled();
    expect(batchUpdate).not.toHaveBeenCalled();
  });

  it("moves no sub-issues when nesting is switched off", () => {
    render({ hierarchy: false });

    drop("parent", "status:done");

    expect(onMoveIssue).toHaveBeenCalled();
    expect(batchUpdate).not.toHaveBeenCalled();
  });
});
