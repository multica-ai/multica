import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

const { mockIssueCliToken, searchParamsState, authStateRef } = vi.hoisted(
  () => ({
    mockIssueCliToken: vi.fn(),
    searchParamsState: { params: new URLSearchParams() },
    authStateRef: {
      state: {
        user: null as null | { id: string; email: string },
        isLoading: false,
      },
    },
  }),
);

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/login",
  useSearchParams: () => searchParamsState.params,
}));

// Use the real sanitizeNextUrl / paths / etc so redirect sanitization is
// exercised rather than silently drifting behind a mock reimplementation.
vi.mock("@multica/core/auth", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/auth")>(
    "@multica/core/auth",
  );
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) =>
      selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

vi.mock("@/features/auth/auth-cookie", () => ({
  setLoggedInCookie: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: vi.fn().mockResolvedValue([]),
    setToken: vi.fn(),
    getMe: vi.fn().mockRejectedValue(new Error("unauthorized")),
    issueCliToken: mockIssueCliToken,
  },
}));

import LoginPage from "./page";

describe("LoginPage (web wrapper)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
  });

  it("renders the Google-only sign-in screen", () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(screen.getByText(/sign in to multica/i)).toBeInTheDocument();
    expect(
      screen.getByText(/sign-in is not configured|sign in with your @g2\.com/i),
    ).toBeInTheDocument();
    // Email / OTP UI must not surface on the public login page.
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^continue$/i }),
    ).not.toBeInTheDocument();
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
});
