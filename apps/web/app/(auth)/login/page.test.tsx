import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { renderToString } from "react-dom/server";
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
  mockSendCode,
  mockVerifyCode,
  mockIssueCliToken,
  searchParamsState,
  authStateRef,
} = vi.hoisted(() => ({
  mockSendCode: vi.fn(),
  mockVerifyCode: vi.fn(),
  mockIssueCliToken: vi.fn(),
  searchParamsState: { params: new URLSearchParams() },
  authStateRef: {
    state: {
      sendCode: vi.fn(),
      verifyCode: vi.fn(),
      user: null as null | { id: string; email: string },
      isLoading: false,
    },
  },
}));

// Mock next/navigation
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
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
  authStateRef.state.sendCode = mockSendCode;
  authStateRef.state.verifyCode = mockVerifyCode;
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
    listWorkspaces: vi.fn().mockResolvedValue([]),
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
    issueCliToken: mockIssueCliToken,
  },
}));

import { configStore } from "@multica/core/config";
import { getBrowserOAuthRedirectUri } from "./oauth-redirect";
import LoginPage from "./page";

describe("LoginPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    configStore.getState().setAuthConfig({ allowSignup: true, oauthProviders: [] });
  });

  it("renders login form with email input and continue button", () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(screen.getByText("Sign in to Multica")).toBeInTheDocument();
    expect(screen.getByText("Enter your email to get a login code")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue" })
    ).toBeInTheDocument();
  });

  it("does not call sendCode when email is empty", async () => {
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(mockSendCode).not.toHaveBeenCalled();
  });

  it("calls sendCode with email on submit", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      expect(mockSendCode).toHaveBeenCalledWith("test@multica.ai");
    });
  });

  it("shows 'Sending code...' while submitting", async () => {
    mockSendCode.mockReturnValueOnce(new Promise(() => {}));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      expect(screen.getByText("Sending code...")).toBeInTheDocument();
    });
  });

  it("shows verification code step after sending code", async () => {
    mockSendCode.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      expect(screen.getByText("Check your email")).toBeInTheDocument();
    });
  });

  it("shows error when sendCode fails", async () => {
    mockSendCode.mockRejectedValueOnce(new Error("Network error"));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@multica.ai");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
  });

  it("renders OAuth providers from runtime config", () => {
    configStore.getState().setAuthConfig({
      allowSignup: true,
      oauthProviders: [
        {
          id: "feishu_lark",
          label: "Feishu/Lark",
          client_id: "cli_lark",
          authorization_url: "https://open.feishu.cn/open-apis/authen/v1/authorize",
        },
      ],
    });

    render(<LoginPage />, { wrapper: createWrapper() });

    expect(
      screen.getByRole("button", { name: "Continue with Feishu/Lark" }),
    ).toBeInTheDocument();
  });

  it("server-renders without reading window during render", () => {
    configStore.getState().setAuthConfig({
      allowSignup: true,
      oauthProviders: [
        {
          id: "feishu_lark",
          label: "Feishu/Lark",
          client_id: "cli_lark",
          authorization_url: "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
        },
      ],
    });

    const originalWindow = globalThis.window;
    vi.stubGlobal("window", undefined);
    try {
      expect(window).toBeUndefined();
      expect(() =>
        renderToString(
          createWrapper()({
            children: <LoginPage />,
          }),
        ),
      ).not.toThrow();
    } finally {
      vi.stubGlobal("window", originalWindow);
    }
  });

  it("returns no OAuth redirect URI when there is no browser window", () => {
    const originalWindow = globalThis.window;
    vi.stubGlobal("window", undefined);
    try {
      expect(getBrowserOAuthRedirectUri()).toBeUndefined();
    } finally {
      vi.stubGlobal("window", originalWindow);
    }
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
