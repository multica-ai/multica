import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { useSidebar } from "@multica/ui/components/ui/sidebar";
import { RESOURCES } from "@multica/views/locales";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

// The shell resolves the mocked `getCurrentSlug()` against the workspace list
// before mounting workspace-scoped chrome, so the list has to contain it or
// the sidebar under test never renders. Gating behaviour itself is covered by
// desktop-layout.workspace-gate.test.tsx.
const WORKSPACES = [{ id: "ws-1", slug: "acme" }];

// 隔离页面内容，保留真实导航适配器和标签存储来验证通知点击。
vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => ({
    canGoBack: false,
    canGoForward: false,
    historyEntries: [],
    historyIndex: 0,
    browsingHistory: [],
    goBack: vi.fn(),
    goForward: vi.fn(),
    goToHistoryIndex: vi.fn(),
  }),
  useNavigationInputBindings: () => {},
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/paths")>(),
  WorkspaceSlugProvider: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  useCurrentWorkspace: () => null,
}));

vi.mock("@multica/core/platform", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/platform")>(),
  getCurrentSlug: () => "acme",
  subscribeToCurrentSlug: () => () => {},
}));

vi.mock("@multica/core/workspace", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspace-list"],
    queryFn: async () => WORKSPACES,
  }),
}));

vi.mock("@multica/views/platform", () => ({
  useDesktopUnreadBadge: () => {},
}));

vi.mock("@multica/views/layout", () => ({
  AppSidebar: () => null,
  GlobalShortcuts: () => null,
  NavigationProgress: () => null,
}));

vi.mock("@multica/views/modals/registry", () => ({ ModalRegistry: () => null }));
vi.mock("@multica/views/search", () => ({
  SearchCommand: () => null,
  SearchTrigger: () => null,
}));
vi.mock("@multica/views/chat", () => ({ FloatingChat: () => null }));
vi.mock("./tab-bar", () => ({ TabBar: () => null }));
vi.mock("./window-overlay", () => ({ WindowOverlay: () => null }));

// Stands in for whatever page the active tab is showing. Reports the one fact
// a `PageHeader` reads before deciding to render its own fallback trigger.
vi.mock("./tab-content", () => ({
  TabContent: () => {
    const { hasExternalTrigger, isCompact, setOpen, state } = useSidebar();
    return (
      <div
        data-compact={isCompact}
        data-external-trigger={hasExternalTrigger}
        data-sidebar-state={state}
        data-testid="page-content"
      >
        <button type="button" onClick={() => setOpen(false)}>
          Collapse sidebar
        </button>
        <button type="button" onClick={() => setOpen(true)}>
          Expand sidebar
        </button>
      </div>
    );
  },
}));

const { DesktopShell } = await import("./desktop-layout");
let inboxOpen: Parameters<typeof window.desktopAPI.onInboxOpen>[0] | null = null;

function renderShell() {
  (
    window as unknown as { desktopAPI: Record<string, unknown> }
  ).desktopAPI = {
    runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } },
    onNavigationGesture: () => () => {},
    onInboxOpen: (listener: NonNullable<typeof inboxOpen>) => {
      inboxOpen = listener;
      return () => { inboxOpen = null; };
    },
  };

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  qc.setQueryData(["workspace-list"], WORKSPACES);

  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        <DesktopShell />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: 1280,
  });
  localStorage.clear();
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
});

describe("DesktopShell notification navigation", () => {
  it.each(["acme", "butter"])("opens a %s notification without replacing existing tabs", (slug) => {
    const store = useTabStore.getState();
    store.switchWorkspace(slug, `/${slug}/projects`);
    store.switchWorkspace("acme");
    const previous = useTabStore.getState().byWorkspace;
    renderShell();

    act(() => inboxOpen!({ slug, itemId: "notification-1", issueKey: "MUL-1" }));

    const updated = useTabStore.getState();
    expect(updated.activeWorkspaceSlug).toBe(slug);
    expect(getActiveTab(updated)?.url).toBe(`/${slug}/inbox?issue=MUL-1`);
    expect(updated.byWorkspace[slug].tabs).toHaveLength(previous[slug].tabs.length + 1);
    for (const tab of previous[slug].tabs) {
      expect(updated.byWorkspace[slug].tabs.find((candidate) => candidate.id === tab.id)).toEqual(tab);
    }
    if (slug !== "acme") expect(updated.byWorkspace.acme).toEqual(previous.acme);

    act(() => inboxOpen!({ slug, itemId: "notification-2", issueKey: "MUL-2" }));
    expect(getActiveTab(useTabStore.getState())?.url).toBe(`/${slug}/inbox?issue=MUL-2`);
    expect(useTabStore.getState().byWorkspace[slug].tabs).toHaveLength(updated.byWorkspace[slug].tabs.length);
  });
});

describe("DesktopShell sidebar trigger", () => {
  // The window toolbar parks a trigger beside the traffic lights that never
  // scrolls away, so nothing inside the canvas may add a second one. Desktop
  // windows sit below `xl`, exactly the band where `PageHeader`'s fallback
  // trigger renders, so every page used to stack an identical icon 50px under
  // this one — and a third when a list/detail surface brought its own header
  // along (MUL-6218).
  it("keeps exactly one trigger and tells page headers not to add another", () => {
    const { container, getByTestId, queryByRole } = renderShell();

    expect(container.querySelectorAll("[data-slot='sidebar-trigger']")).toHaveLength(1);
    expect(getByTestId("page-content")).toHaveAttribute(
      "data-external-trigger",
      "true",
    );
    const mainTopBar = container.querySelector('[data-slot="main-top-bar"]');
    expect(mainTopBar).toHaveAttribute("data-sidebar-resize-consumer");
    expect(mainTopBar).toHaveClass("transition-[padding-left]");
    expect(mainTopBar).toHaveStyle({
      paddingLeft:
        "max(0px, calc(200px - var(--sidebar-live-width, var(--sidebar-width))))",
    });

    fireEvent.click(
      container.querySelector('[data-slot="sidebar-trigger"]')!,
    );
    expect(mainTopBar).toHaveStyle({ paddingLeft: "200px" });
    expect(queryByRole("status", { name: /loading workspace/i })).toBeNull();
  });

  it("keeps full clearance for compact layouts in either sidebar state", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 800,
    });

    const { container, getByRole, getByTestId } = renderShell();
    const mainTopBar = container.querySelector('[data-slot="main-top-bar"]');

    expect(getByTestId("page-content")).toHaveAttribute("data-compact", "true");
    expect(getByTestId("page-content")).toHaveAttribute(
      "data-sidebar-state",
      "expanded",
    );
    expect(mainTopBar).toHaveStyle({ paddingLeft: "200px" });

    fireEvent.click(getByRole("button", { name: "Collapse sidebar" }));
    expect(getByTestId("page-content")).toHaveAttribute(
      "data-sidebar-state",
      "collapsed",
    );
    expect(mainTopBar).toHaveStyle({ paddingLeft: "200px" });

    fireEvent.click(getByRole("button", { name: "Expand sidebar" }));
    expect(getByTestId("page-content")).toHaveAttribute(
      "data-sidebar-state",
      "expanded",
    );
    expect(mainTopBar).toHaveStyle({ paddingLeft: "200px" });
  });
});
