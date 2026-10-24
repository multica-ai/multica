import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import type { ApiClient } from "@multica/core/api/client";
import { IssueViewSchema, type IssueView } from "@multica/core/api/schemas";
import { issueViewPrefKeys } from "@multica/core/issue-views/preferences";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../../test/i18n";
import { ViewBar, type ViewBarBuiltin } from "./view-bar";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const scope = { scope_type: "project" as const, scope_id: "project-1" };
const builtins: ViewBarBuiltin[] = [
  { key: "all", label: "All issues", active: true, onSelect: vi.fn() },
  { key: "board", label: "Board", active: false, onSelect: vi.fn() },
  { key: "timeline", label: "Timeline", active: false, onSelect: vi.fn() },
];
const views = [
  IssueViewSchema.parse({ id: "view-1", name: "My issues" }),
  IssueViewSchema.parse({ id: "view-2", name: "Recently updated" }),
];

let queryClient: QueryClient;
let containerWidth: number;
const resizeObservers = new Set<TestResizeObserver>();

class TestResizeObserver {
  private targets = new Set<Element>();

  constructor(private callback: ResizeObserverCallback) {
    resizeObservers.add(this);
  }

  observe(target: Element) {
    this.targets.add(target);
  }

  unobserve(target: Element) {
    this.targets.delete(target);
  }

  disconnect() {
    resizeObservers.delete(this);
  }

  notify(target: Element) {
    if (this.targets.has(target)) {
      this.callback(
        [{ target } as ResizeObserverEntry],
        this as unknown as ResizeObserver,
      );
    }
  }
}

function renderBar({
  initialViews = views,
  activeView = null,
  viewsReady = true,
}: {
  initialViews?: IssueView[];
  activeView?: IssueView | null;
  viewsReady?: boolean;
} = {}) {
  const props = {
    wsId: "ws-1",
    scope,
    builtins,
    activeView,
    onSelectView: vi.fn(),
    onNewView: vi.fn(),
    onEditView: vi.fn(),
  };
  const result = renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <ViewBar {...props} views={initialViews} viewsReady={viewsReady} />
    </QueryClientProvider>,
  );
  return {
    ...result,
    loadViews: () => result.rerender(
      <QueryClientProvider client={queryClient}>
        <ViewBar {...props} views={views} viewsReady />
      </QueryClientProvider>,
    ),
    resize: (width: number) => {
      act(() => {
        containerWidth = width;
        for (const observer of resizeObservers) {
          observer.notify(result.container.firstElementChild!);
        }
      });
    },
  };
}

beforeEach(() => {
  registerAuthStore(
    createAuthStore({
      api: {} as ApiClient,
      storage: {
        getItem: () => null,
        setItem: () => {},
        removeItem: () => {},
      },
    }),
  );
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
    },
  });
  queryClient.setQueryData(workspaceKeys.members("ws-1"), []);
  queryClient.setQueryData(issueViewPrefKeys.scope("ws-1", scope), {
    scope_type: "project",
    scope_id: scope.scope_id,
    prefs: {
      hidden: [],
      order: [
        ...builtins.map((item) => `builtin:${item.key}`),
        ...views.map((view) => `view:${view.id}`),
      ],
    },
    updated_at: "",
  });
  // Five 100px tabs fit with the permanent menu, but not with an overflow
  // trigger: 5 * 100 + 5 * 4 + 36 <= 570 < 5 * 100 + 5 * 4 + 68.
  containerWidth = 570;
  vi.stubGlobal("ResizeObserver", TestResizeObserver);
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(
    () => containerWidth,
  );
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(100);
});

afterEach(() => {
  cleanup();
  queryClient.clear();
  resizeObservers.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ViewBar fitting", () => {
  it("settles when saved views load at the overflow boundary", () => {
    const { loadViews } = renderBar({ initialViews: [], viewsReady: false });
    expect(loadViews).not.toThrow();

    for (const item of [...builtins, ...views.map((view) => ({ label: view.name }))]) {
      expect(screen.getByRole("button", { name: item.label })).toBeInTheDocument();
    }
    expect(screen.queryByRole("button", { name: "More views" })).not.toBeInTheDocument();
  });

  it("restores all tabs when the container grows past the overflow boundary", () => {
    containerWidth = 400;
    const { resize } = renderBar();

    expect(screen.getByRole("button", { name: "More views" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: views[0]!.name })).not.toBeInTheDocument();

    resize(570);

    for (const item of [...builtins, ...views.map((view) => ({ label: view.name }))]) {
      expect(screen.getByRole("button", { name: item.label })).toBeInTheDocument();
    }
    expect(screen.queryByRole("button", { name: "More views" })).not.toBeInTheDocument();
  });

  it("keeps the active overflowed view available by name", () => {
    containerWidth = 450;
    renderBar({ activeView: views[1]! });

    expect(screen.getByRole("button", { name: "All issues" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Board" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Timeline" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: views[1]!.name })).toBeInTheDocument();
  });

  it("keeps overflow controls available when no tab fits", () => {
    containerWidth = 80;
    renderBar();

    expect(screen.getByRole("button", { name: "More views" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Views" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "All issues" })).not.toBeInTheDocument();
  });
});
