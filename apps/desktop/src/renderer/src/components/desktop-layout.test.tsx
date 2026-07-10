import { render } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("@/hooks/use-tab-history", () => ({
  useTabHistory: () => ({
    canGoBack: false,
    canGoForward: false,
    goBack: vi.fn(),
    goForward: vi.fn(),
  }),
}));

vi.mock("@/hooks/use-tab-sync", () => ({
  useActiveTitleSync: () => undefined,
}));

vi.mock("@/stores/tab-store", () => ({
  resolveRouteIcon: () => null,
  useTabStore: Object.assign(() => null, {
    getState: () => ({
      openTab: vi.fn(() => "tab-1"),
      setActiveTab: vi.fn(),
    }),
  }),
}));

vi.mock("@multica/ui/components/ui/sidebar", () => ({
  SidebarProvider: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SidebarTrigger: () => <button type="button">Sidebar</button>,
  useSidebar: () => ({ state: "expanded", isMobile: false }),
}));

vi.mock("@multica/views/modals/registry", () => ({
  ModalRegistry: () => null,
}));

vi.mock("@multica/views/layout", () => ({
  AppSidebar: () => null,
}));

vi.mock("@multica/views/search", () => ({
  SearchCommand: () => null,
  SearchTrigger: () => null,
}));

vi.mock("@multica/core/paths", () => ({
  WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  paths: {
    workspace: (slug: string) => ({
      inbox: () => `/${slug}/inbox`,
    }),
  },
  useCurrentWorkspace: () => null,
}));

vi.mock("@multica/core/platform", () => ({
  getCurrentSlug: () => null,
  subscribeToCurrentSlug: () => () => undefined,
}));

vi.mock("@multica/views/platform", () => ({
  useDesktopUnreadBadge: vi.fn(),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("@/platform/navigation", () => ({
  DesktopNavigationProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => {
    throw new Error("useWorkspaceId: no workspace selected");
  },
}));

vi.mock("./tab-bar", () => ({
  TabBar: () => null,
}));

vi.mock("./tab-content", () => ({
  TabContent: () => <div data-testid="tab-content" />,
}));

vi.mock("./window-overlay", () => ({
  WindowOverlay: () => null,
}));

import { DesktopShell } from "./desktop-layout";

beforeEach(() => {
  window.desktopAPI = {
    onNavigationGesture: vi.fn(() => () => undefined),
    onInboxOpen: vi.fn(() => () => undefined),
  } as unknown as typeof window.desktopAPI;
});

describe("DesktopShell", () => {
  it("renders before a workspace is selected without mounting workspace-scoped browser bridge", () => {
    expect(() => render(<DesktopShell />)).not.toThrow();
  });
});
