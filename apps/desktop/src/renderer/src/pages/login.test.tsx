import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";

const mocks = vi.hoisted(() => ({
  loginWithToken: vi.fn(),
  logout: vi.fn(),
  listWorkspaces: vi.fn(),
  openExternal: vi.fn(),
}));

vi.mock("@multica/views/auth", () => ({
  LoginPage: ({
    extra,
    onGoogleLogin,
  }: {
    extra?: ReactNode;
    onGoogleLogin?: () => void;
  }) => (
    <div>
      <div data-testid="login-form" />
      <button type="button" onClick={onGoogleLogin}>
        Google login
      </button>
      {extra}
    </div>
  ),
}));

vi.mock("@multica/views/platform", () => ({ DragStrip: () => null }));
vi.mock("@multica/ui/components/common/multica-icon", () => ({
  MulticaIcon: () => null,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: {
    getState: () => ({
      loginWithToken: mocks.loginWithToken,
      logout: mocks.logout,
    }),
  },
}));
vi.mock("@multica/core/api", () => ({
  api: { listWorkspaces: mocks.listWorkspaces },
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

const { DesktopLoginPage } = await import("./login");

function renderPage(apiUrl = "http://localhost:8080") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  (window as unknown as { desktopAPI: Record<string, unknown> }).desktopAPI = {
    runtimeConfig: {
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl,
        wsUrl: "ws://localhost:8080/ws",
        appUrl: "http://localhost:3000",
      },
    },
    openExternal: mocks.openExternal,
  };

  render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <DesktopLoginPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return queryClient;
}

beforeEach(() => {
  vi.stubEnv("VITE_DESKTOP_DEV_AUTH_TOKEN", "local-profile-token");
  mocks.loginWithToken.mockResolvedValue({ id: "user-1" });
  mocks.listWorkspaces.mockResolvedValue([{ id: "ws-1", slug: "dev" }]);
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.clearAllMocks();
});

describe("DesktopLoginPage local profile login", () => {
  it("keeps the login form and shows Skip login for a loopback dev API", () => {
    renderPage();

    expect(screen.getByTestId("login-form")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Skip login" }),
    ).toBeInTheDocument();
  });

  it("does not expose Skip login for a remote API", () => {
    renderPage("https://api.multica.ai");

    expect(screen.getByTestId("login-form")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Skip login" }),
    ).not.toBeInTheDocument();
  });

  it("uses the local profile and warms the workspace cache", async () => {
    const queryClient = renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Skip login" }));

    await waitFor(() => {
      expect(mocks.loginWithToken).toHaveBeenCalledWith("local-profile-token");
      expect(mocks.listWorkspaces).toHaveBeenCalledTimes(1);
      expect(queryClient.getQueryData(["workspaces"])).toEqual([
        { id: "ws-1", slug: "dev" },
      ]);
    });
  });

  it("keeps the login form usable when the local profile fails", async () => {
    mocks.loginWithToken.mockRejectedValueOnce(new Error("invalid token"));
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Skip login" }));

    expect(
      await screen.findByRole("alert"),
    ).toHaveTextContent("Could not use the local development profile.");
    expect(mocks.logout).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("login-form")).toBeInTheDocument();
  });
});
