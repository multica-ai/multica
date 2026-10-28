/**
 * @vitest-environment jsdom
 *
 * Board sub-issue nesting: sub-issues render inside their parent's card,
 * recursively; in another column a muted ghost frame names the parent; and a
 * parent folds its sub-issues away.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type { Issue, IssueStatus } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../actions/issue-actions-context-menu";
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

// Server order puts the same-column child after an unrelated card, so nesting
// has to pull it into its parent; the done child lives in another column.
const ISSUES = [
  makeIssue("parent", "todo"),
  makeIssue("other", "todo"),
  makeIssue("child", "todo", "parent"),
  makeIssue("grandchild", "todo", "child"),
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

function column(title: string) {
  return screen.getByText(title).closest("div.flex-col") as HTMLElement;
}

function order(container: HTMLElement, ids: string[]) {
  const text = container.textContent ?? "";
  return [...ids].sort((a, b) => text.indexOf(`MUL-${a}`) - text.indexOf(`MUL-${b}`));
}

describe("Board sub-issue nesting", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  function render() {
    const store = getIssueSurfaceViewStore(
      `board-hierarchy-${Math.floor(Math.random() * 1e9)}`,
    );
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueContextMenuProvider>
            <BoardView
              issues={ISSUES}
              visibleStatuses={["todo", "done"]}
              onMoveIssue={() => {}}
            />
          </IssueContextMenuProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );
    return store;
  }

  /** The card that hosts the given issue's own card, if any. */
  function hostCard(identifier: string) {
    const own = screen.getByText(identifier).closest("[data-board-card]");
    return own?.parentElement?.closest("[data-board-card]") ?? null;
  }

  it("renders sub-issues inside their parent's card, recursively", () => {
    render();

    expect(hostCard("MUL-child")?.textContent).toContain("MUL-parent");
    expect(hostCard("MUL-grandchild")?.textContent).toContain("MUL-child");
    expect(hostCard("MUL-other")).toBeNull();
    expect(order(column("Todo"), ["parent", "other", "child", "grandchild"])).toEqual([
      "parent",
      "child",
      "grandchild",
      "other",
    ]);
  });

  it("frames a sub-issue in another column with its parent's title", () => {
    render();

    const done = column("Done");
    expect(done.textContent).toContain("MUL-finished");
    // The ghost names the parent but is not a card of its own there.
    expect(done.textContent).toContain("Task parent");
    expect(hostCard("MUL-finished")).toBeNull();
    expect(column("Todo").textContent).not.toContain("MUL-finished");
  });

  it("folds a parent's nested sub-issues away and back", () => {
    render();

    const toggle = screen.getByRole("button", { name: "2 sub-issues" });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(column("Todo").textContent).not.toContain("MUL-child");
    expect(column("Todo").textContent).not.toContain("MUL-grandchild");

    fireEvent.click(toggle);
    expect(column("Todo").textContent).toContain("MUL-grandchild");
  });

  it("keeps flat server order when nesting is switched off", () => {
    const store = render();

    act(() => store.getState().toggleBoardHierarchy());

    expect(screen.queryByRole("button", { name: "2 sub-issues" })).toBeNull();
    expect(hostCard("MUL-child")).toBeNull();
    expect(column("Done").textContent).not.toContain("Task parent");
    expect(order(column("Todo"), ["parent", "other", "child", "grandchild"])).toEqual([
      "parent",
      "other",
      "child",
      "grandchild",
    ]);
  });
});
