import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";

/**
 * Regression guard for MUL-6231 / #7021: deleting the last workspace blanked
 * the desktop client.
 *
 * The shell used to decide whether to mount workspace-scoped chrome from the
 * `getCurrentSlug()` singleton alone. That singleton is mutable process state
 * with no owner keeping it in lockstep with the workspace list, so after the
 * active workspace was deleted it kept returning the dead slug — and the shell
 * kept `SearchCommand` mounted, whose `useWorkspaceId()` THROWS once the
 * workspace leaves the list. Nothing above the shell was an error boundary, so
 * that throw unmounted the entire renderer: a blank, unresponsive window.
 *
 * The invariant these tests lock is "a slug the workspace list no longer
 * contains mounts no workspace-scoped chrome", which is the same signal web's
 * DashboardGuard already gates on.
 */

const state = vi.hoisted(() => ({
  currentSlug: "acme" as string | null,
  wsList: [] as { id: string; slug: string }[],
}));

vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => ({
    canGoBack: false,
    canGoForward: false,
    goBack: vi.fn(),
    goForward: vi.fn(),
  }),
  useNavigationInputBindings: () => {},
}));

vi.mock("@/platform/navigation", () => ({
  DesktopNavigationProvider: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  routeContentLinkPath: vi.fn(),
}));

vi.mock("@multica/core/paths", () => ({
  WorkspaceSlugProvider: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  paths: { workspace: () => ({ inbox: () => "/acme/inbox" }) },
  useCurrentWorkspace: () => null,
}));

vi.mock("@multica/core/platform", () => ({
  getCurrentSlug: () => state.currentSlug,
  subscribeToCurrentSlug: () => () => {},
}));

vi.mock("@multica/core/workspace", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspace-list"],
    queryFn: async () => state.wsList,
  }),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("@multica/views/platform", () => ({
  useDesktopUnreadBadge: () => {},
}));

// Each workspace-scoped component gets a marker so the assertions can tell
// which of them the shell decided to mount.
vi.mock("@multica/views/layout", () => ({
  AppSidebar: () => <div data-testid="app-sidebar" />,
  GlobalShortcuts: () => <div data-testid="global-shortcuts" />,
}));

vi.mock("@multica/views/modals/registry", () => ({
  ModalRegistry: () => <div data-testid="modal-registry" />,
}));

// Stands in for the real SearchCommand, which calls useWorkspaceId() at the
// top of its body. Mounting it without a resolvable workspace is precisely
// the crash this gate prevents, so the stub throws the same way.
vi.mock("@multica/views/search", () => ({
  SearchCommand: () => {
    const resolved = state.wsList.some((w) => w.slug === state.currentSlug);
    if (!resolved) {
      throw new Error(
        "useWorkspaceId: no workspace selected — ensure component renders inside a workspace route",
      );
    }
    return <div data-testid="search-command" />;
  },
  SearchTrigger: () => null,
}));

vi.mock("@multica/views/chat", () => ({
  FloatingChat: () => <div data-testid="floating-chat" />,
}));

vi.mock("./tab-bar", () => ({ TabBar: () => null }));
vi.mock("./window-overlay", () => ({ WindowOverlay: () => null }));
vi.mock("./tab-content", () => ({
  TabContent: () => <div data-testid="tab-content" />,
}));

const { DesktopShell } = await import("./desktop-layout");

function renderShell() {
  (
    window as unknown as { desktopAPI: Record<string, unknown> }
  ).desktopAPI = {
    onNavigationGesture: () => () => {},
    onInboxOpen: () => () => {},
  };

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  qc.setQueryData(["workspace-list"], state.wsList);

  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        <DesktopShell />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.currentSlug = "acme";
  state.wsList = [{ id: "ws-1", slug: "acme" }];
});

describe("DesktopShell workspace gating", () => {
  it("mounts workspace-scoped chrome while the slug resolves", () => {
    const { queryByTestId } = renderShell();

    expect(queryByTestId("app-sidebar")).not.toBeNull();
    expect(queryByTestId("search-command")).not.toBeNull();
    expect(queryByTestId("global-shortcuts")).not.toBeNull();
    expect(queryByTestId("modal-registry")).not.toBeNull();
    expect(queryByTestId("floating-chat")).not.toBeNull();
  });

  it("drops workspace-scoped chrome when the singleton still points at a deleted workspace", () => {
    // Exactly the post-delete window: the list refetch has landed empty, but
    // nothing has cleared the slug singleton yet.
    state.wsList = [];

    const { queryByTestId } = renderShell();

    expect(queryByTestId("app-sidebar")).toBeNull();
    expect(queryByTestId("search-command")).toBeNull();
    expect(queryByTestId("global-shortcuts")).toBeNull();
    expect(queryByTestId("modal-registry")).toBeNull();
    expect(queryByTestId("floating-chat")).toBeNull();
  });

  it("keeps TabContent mounted with no workspace so the tab router can still resolve one", () => {
    state.wsList = [];

    const { queryByTestId } = renderShell();

    expect(queryByTestId("tab-content")).not.toBeNull();
  });

  it("drops chrome for a slug belonging to a workspace the user lost access to", () => {
    state.currentSlug = "gone";
    state.wsList = [{ id: "ws-1", slug: "acme" }];

    const { queryByTestId } = renderShell();

    expect(queryByTestId("app-sidebar")).toBeNull();
    expect(queryByTestId("search-command")).toBeNull();
  });
});
