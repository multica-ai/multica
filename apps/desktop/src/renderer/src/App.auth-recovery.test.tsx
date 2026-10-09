import type { ReactNode } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  auth: {
    isLoading: false,
    retryAuthentication: vi.fn(),
    status: "authenticated",
    user: { id: "user-1" } as { id: string } | null,
  },
  refetchWorkspaceList: vi.fn(),
  workspaceListUnavailable: false,
}));

const tabStore = vi.hoisted(() => ({
  activeWorkspaceSlug: "acme" as string | null,
  byWorkspace: {},
  closeActiveTab: vi.fn(),
  reset: vi.fn(),
  switchWorkspace: vi.fn(),
  validateWorkspaceSlugs: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: vi.fn() }),
}));

vi.mock("@multica/core/platform", () => ({
  CoreProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/i18n", () => ({
  pickLocale: () => "en",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (auth: typeof state.auth) => unknown) =>
    selector(state.auth),
}));

vi.mock("@multica/core/onboarding", () => ({
  useWelcomeStore: { getState: () => ({ reset: vi.fn() }) },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspace-list"] },
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({
    isFetching: false,
    ready: true,
    refetch: state.refetchWorkspaceList,
    unavailable: state.workspaceListUnavailable,
    workspaces: [{ id: "ws-1", slug: "acme" }],
  }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listMyInvitations: vi.fn(),
    listWorkspaces: vi.fn(),
  },
}));

vi.mock("@multica/core/paths", () => ({
  useHasOnboarded: () => true,
}));

vi.mock("@multica/core/analytics", () => ({ captureEvent: vi.fn() }));
vi.mock("@multica/ui/components/common/theme-provider", () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/ui/components/common/multica-icon", () => ({
  MulticaIcon: () => <div data-testid="app-loading" />,
}));
vi.mock("@multica/ui/components/ui/sonner", () => ({ Toaster: () => null }));
vi.mock("@multica/views/locales", () => ({ RESOURCES: { en: {} } }));

vi.mock("./pages/login", () => ({
  DesktopLoginPage: () => <div data-testid="login-page" />,
}));
vi.mock("./pages/auth-recovery", () => ({
  DesktopAuthRecoveryPage: ({
    isRetrying = false,
    onRetry,
  }: {
    isRetrying?: boolean;
    onRetry?: () => void;
  }) => (
    <div data-testid="auth-recovery" data-retrying={isRetrying}>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry workspace list
        </button>
      )}
    </div>
  ),
}));
vi.mock("./components/desktop-layout", () => ({
  DesktopShell: () => <div data-testid="desktop-shell" />,
}));
vi.mock("./components/update-notification", () => ({
  UpdateNotification: () => null,
}));
vi.mock("./components/issue-window", () => ({
  IssueWindow: () => <div data-testid="issue-window" />,
}));

vi.mock("./stores/tab-store", () => ({
  useTabStore: Object.assign(
    (selector: (value: typeof tabStore) => unknown) => selector(tabStore),
    { getState: () => tabStore },
  ),
}));
vi.mock("./stores/window-overlay-store", () => ({
  useWindowOverlayStore: {
    getState: () => ({ close: vi.fn(), open: vi.fn(), overlay: null }),
  },
}));

vi.mock("./hooks/use-open-settings-shortcut", () => ({
  useOpenSettingsShortcut: () => {},
}));
vi.mock("./hooks/use-tab-selection-shortcut", () => ({
  useTabSelectionShortcut: () => {},
}));
vi.mock("./platform/daemon-ipc-bridge", () => ({
  useDaemonIPCBridge: () => {},
}));
vi.mock("./platform/daemon-login-sync", () => ({
  syncDaemonOnLogin: vi.fn(),
}));
vi.mock("./platform/i18n-adapter", () => ({
  createDesktopLocaleAdapter: () => ({
    getSystemPreferences: () => ["en"],
    getUserChoice: () => null,
    persist: vi.fn(),
  }),
}));
vi.mock("./platform/client-usage-reporter", () => ({
  DesktopClientUsageReporter: () => null,
}));
vi.mock("./platform/diagnostic-route-reporter", () => ({
  DiagnosticRouteReporter: () => null,
}));
vi.mock("./freeze-flush", () => ({ flushFreezeBreadcrumb: vi.fn() }));
vi.mock("./platform/auth-session-bridge", () => ({
  DesktopAuthSessionBridge: () => null,
}));
vi.mock("./platform/session-teardown", () => ({
  tearDownOnLogout: vi.fn(),
  tearDownOnSessionExpiry: vi.fn(),
}));

const { default: App } = await import("./App");

beforeEach(() => {
  state.auth.isLoading = false;
  state.auth.status = "authenticated";
  state.auth.user = { id: "user-1" };
  state.refetchWorkspaceList.mockReset();
  state.workspaceListUnavailable = false;
  tabStore.activeWorkspaceSlug = "acme";

  Object.assign(window, {
    daemonAPI: {
      clearToken: vi.fn(),
      restart: vi.fn(),
      setTargetApiUrl: vi.fn(),
      stop: vi.fn(),
    },
    desktopAPI: {
      ackFreeze: vi.fn(),
      appInfo: { os: "macos", version: "0.5.1" },
      closeWindow: vi.fn(),
      getLastFreeze: vi.fn(),
      onAuthToken: () => () => {},
      onCloseActiveTab: () => () => {},
      onInviteOpen: () => () => {},
      onSystemLocaleChanged: () => () => {},
      reportAuthSession: vi.fn(),
      runtimeConfig: {
        config: { apiUrl: "http://localhost", wsUrl: "ws://localhost" },
        ok: true,
      },
      systemLocale: "en",
      windowContext: { kind: "main" },
    },
  });
});

describe("App main-window auth recovery", () => {
  it("keeps auth recovery ahead of the desktop shell while the session recovers", () => {
    state.auth.status = "recovering";

    render(<App />);

    expect(screen.getByTestId("auth-recovery")).toBeInTheDocument();
    expect(screen.queryByTestId("desktop-shell")).toBeNull();
  });

  it("uses recovery with retry when the workspace list is unavailable", () => {
    state.workspaceListUnavailable = true;

    render(<App />);

    expect(screen.getByTestId("auth-recovery")).toBeInTheDocument();
    expect(screen.queryByTestId("desktop-shell")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "Retry workspace list" }),
    );
    expect(state.refetchWorkspaceList).toHaveBeenCalledOnce();
  });

  it("keeps auth recovery ahead of a dedicated issue window", () => {
    state.auth.status = "recovering";
    window.desktopAPI.windowContext = {
      issueId: "MUL-1",
      kind: "issue",
      path: "/acme/issues/MUL-1",
      title: "MUL-1",
      workspaceSlug: "acme",
    };

    render(<App />);

    expect(screen.getByTestId("auth-recovery")).toBeInTheDocument();
    expect(screen.queryByTestId("issue-window")).toBeNull();
  });
});
