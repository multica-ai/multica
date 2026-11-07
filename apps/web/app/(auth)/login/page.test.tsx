import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { configStore } from "@multica/core/config";
import { takeGoogleReturnTo } from "@/features/auth/return-to";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "@multica/views/locales/en/common.json";
import enAuth from "@multica/views/locales/en/auth.json";
import enSettings from "@multica/views/locales/en/settings.json";
import type { ReactNode } from "react";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const {
  mockIssueCliToken,
  mockListWorkspaces,
  mockListMyInvitations,
  mockPush,
  mockReplace,
  searchParamsState,
  authStateRef,
} = vi.hoisted(() => ({
  mockIssueCliToken: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockListMyInvitations: vi.fn(),
  mockPush: vi.fn(),
  mockReplace: vi.fn(),
  searchParamsState: { params: new URLSearchParams() },
  authStateRef: {
    state: {
      sendCode: vi.fn(),
      verifyCode: vi.fn(),
      user: null as null | { id: string; email: string; onboarded_at?: string | null },
      isLoading: false,
    },
  },
}));

// Mock next/navigation — router spies are hoisted so tests can assert
// which navigation (if any) the page issued.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  usePathname: () => "/login",
  useSearchParams: () => searchParamsState.params,
}));

// Mock auth store — shared LoginPage uses getState().sendCode/verifyCode,
// web wrapper uses useAuthStore((s) => s.user/isLoading). Keep the real
// sanitizeNextUrl so the redirect-sanitization rules are exercised rather
// than silently drifting behind a mock reimplementation.
vi.mock("@multica/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/auth")>(
      "@multica/core/auth",
    );
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) =>
      selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

// Mock auth-cookie
vi.mock("@/features/auth/auth-cookie", () => ({
  setLoggedInCookie: vi.fn(),
}));

// Mock api
vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    listMyInvitations: mockListMyInvitations,
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
    issueCliToken: mockIssueCliToken,
  },
}));

import LoginPage from "./page";

describe("LoginPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);
  });

  it.each([null, { id: "u1", email: "test@multica.ai" }])(
    "blocks invalid CLI callbacks even with an existing session: %j",
    (user) => {
      searchParamsState.params = new URLSearchParams({
        cli_callback: "http://198.18.0.1:46233/callback",
        cli_state: "test-state",
        platform: "desktop",
      });
      authStateRef.state.user = user;
      render(<LoginPage />, { wrapper: createWrapper() });
      expect(screen.getByText("CLI authorization incomplete")).toBeInTheDocument();
      expect(screen.queryByLabelText("Email")).not.toBeInTheDocument();
      expect(mockIssueCliToken).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
      expect(mockReplace).not.toHaveBeenCalled();
    },
  );

  it("blocks an explicitly empty CLI callback", () => {
    searchParamsState.params = new URLSearchParams({ cli_callback: "" });
    render(<LoginPage />, { wrapper: createWrapper() });
    expect(screen.getByText("CLI authorization incomplete")).toBeInTheDocument();
  });

  // Shared LoginPage behavior is canonical in
  // packages/views/auth/login-page.test.tsx. This wrapper suite only owns web
  // platform handoff and redirect behavior.

  it("keeps the complete return address in the tab when starting Google OAuth", async () => {
    const next = "/acme/settings?tab=billing&session_id=example-secret#receipt";
    searchParamsState.params = new URLSearchParams({ next });
    configStore.getState().setAuthConfig({ allowSignup: true, googleClientId: "test-client" });
    const originalLocation = window.location;
    const navigate = vi.fn();
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, set href(value: string) { navigate(value); } },
    });

    try {
      render(<LoginPage />, { wrapper: createWrapper() });
      await act(async () => {
        fireEvent.click(screen.getByRole("button", { name: "Continue with Google" }));
      });
      expect(navigate).toHaveBeenCalledOnce();
      const oauth = new URL(navigate.mock.calls[0]![0]);
      const state = oauth.searchParams.get("state") ?? "";
      expect(state).toMatch(/^next_ref:[a-f0-9]{32}$/);
      expect(oauth.href).not.toContain("example-secret");
      expect(takeGoogleReturnTo(state.slice("next_ref:".length))).toBe(next);
    } finally {
      Object.defineProperty(window, "location", { configurable: true, value: originalLocation });
      configStore.getState().setAuthConfig({ allowSignup: true });
    }
  });

  it("resolves a legacy bookmark and its inherited fragment after login", async () => {
    const original = window.location.href;
    const params = new URLSearchParams({ next: "/projects/project-1?view=members" });
    searchParamsState.params = params;
    authStateRef.state.user = { id: "u1", email: "test@example.invalid", onboarded_at: "2026-01-01T00:00:00Z" };
    mockListWorkspaces.mockResolvedValue([{ id: "ws-1", slug: "acme" }]);
    window.history.replaceState(null, "", `/login?${params}#activity`);

    try {
      render(<LoginPage />, { wrapper: createWrapper() });
      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith("/acme/projects/project-1?view=members#activity");
      });
    } finally {
      window.history.replaceState(null, "", original);
    }
  });

  it("uses the normal landing page when a legacy bookmark has no workspace", async () => {
    searchParamsState.params = new URLSearchParams({ next: "/projects/project-1?view=members" });
    authStateRef.state.user = { id: "u1", email: "test@example.invalid", onboarded_at: "2026-01-01T00:00:00Z" };

    render(<LoginPage />, { wrapper: createWrapper() });
    await waitFor(() => expect(mockReplace).toHaveBeenCalledExactlyOnceWith("/workspaces/new"));
  });

  // Regression: MUL-1080 — if the user is already authenticated on the web
  // and the Desktop app redirects them to /login?platform=desktop, the web
  // must exchange the cookie session for a bearer token and hand it off via
  // the multica:// deep link, not silently redirect to the workspace page.
  it("mints a token and deep-links to Desktop when already logged in with platform=desktop", async () => {
    searchParamsState.params = new URLSearchParams({ platform: "desktop" });
    authStateRef.state.user = { id: "u1", email: "test@multica.ai" };
    mockIssueCliToken.mockImplementation(() =>
      Promise.resolve({ token: "handoff-jwt" }),
    );

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockIssueCliToken).toHaveBeenCalledTimes(1);
      });
      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "multica://auth/callback?token=handoff-jwt",
        );
      });
      expect(
        await screen.findByRole("button", { name: "Open Multica Desktop" }),
      ).toBeInTheDocument();
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  // Regression: #5009 — the "already authenticated on arrival" effect used to
  // fire for fresh form logins too. verifyCode writes `user` while handleVerify
  // is still fetching the workspace list, so the effect read an empty cache and
  // raced handleSuccess with replace("/workspaces/new"); depending on the
  // interleaving the user could end up stuck on the create-workspace page
  // despite having workspaces.
  describe("post-login redirect ownership (#5009)", () => {
    const onboardedUser = {
      id: "u1",
      email: "test@multica.ai",
      onboarded_at: "2026-01-01T00:00:00Z",
    };

    it("does not redirect from the arrival effect when the user logs in via the form", async () => {
      // Auth settles as logged-out first — the page latches "any user from
      // now on came from the form".
      const wrapper = createWrapper();
      const { rerender } = render(<LoginPage />, { wrapper });
      // verifyCode set the user; the workspace list fetch is still in flight
      // (cache cold). The arrival effect must stay silent — handleSuccess
      // owns this navigation.
      authStateRef.state.user = onboardedUser;
      rerender(<LoginPage />);

      await act(async () => {});
      expect(mockReplace).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });

    it("fetches the workspace list before redirecting a visitor who arrived authenticated", async () => {
      // Cold Query cache on a fresh page load: reading it would say "no
      // workspaces" and misroute to /workspaces/new. The effect must fetch.
      authStateRef.state.user = onboardedUser;
      mockListWorkspaces.mockResolvedValue([{ id: "ws-1", slug: "acme" }]);

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith("/acme/issues");
      });
      expect(mockListWorkspaces).toHaveBeenCalledTimes(1);
    });

    it("still honors ?next= for a visitor who arrived authenticated", async () => {
      searchParamsState.params = new URLSearchParams({
        next: "/invite/abc",
      });
      authStateRef.state.user = onboardedUser;

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith("/invite/abc");
      });
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });
  });
});
