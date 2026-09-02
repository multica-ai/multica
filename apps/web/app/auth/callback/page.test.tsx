import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { I18nProvider } from "@multica/core/i18n/react";
import { paths } from "@multica/core/paths";
import { RESOURCES } from "@multica/views/locales";
import { ApiError } from "@multica/core/api";

const {
  mockPush,
  mockRouter,
  mockSearchParams,
  mockLoginWithGoogle,
  mockListWorkspaces,
  mockListMyInvitations,
  mockSetQueryData,
  mockQueryClient,
} = vi.hoisted(() => {
  const mockPush = vi.fn();
  const mockSetQueryData = vi.fn();
  return {
    mockPush,
    mockRouter: { push: mockPush },
    mockSearchParams: new URLSearchParams(),
    mockLoginWithGoogle: vi.fn(),
    mockListWorkspaces: vi.fn(),
    mockListMyInvitations: vi.fn(),
    mockSetQueryData,
    mockQueryClient: { setQueryData: mockSetQueryData },
  };
});

vi.mock("@multica/core/logger", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/logger")>(
      "@multica/core/logger",
    );
  return {
    ...actual,
    createLogger: () => ({
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    }),
  };
});

const makeUser = (
  overrides: Partial<{
    onboarded_at: string | null;
    onboarding_questionnaire: Record<string, unknown>;
  }> = {},
) => ({
  id: "user-1",
  name: "Test",
  email: "test@multica.ai",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: { source: ["search"] },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

vi.mock("next/navigation", () => ({
  useRouter: () => mockRouter,
  useSearchParams: () => mockSearchParams,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => mockQueryClient,
}));

// Preserve the real sanitizeNextUrl so the "drop unsafe ?next=" behavior is
// exercised rather than silently diverging from the source of truth.
vi.mock("@multica/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/auth")>(
      "@multica/core/auth",
    );
  return {
    ...actual,
    useAuthStore: (selector: (s: unknown) => unknown) =>
      selector({ loginWithGoogle: mockLoginWithGoogle }),
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: {
    list: () => ["workspaces"],
    myInvitations: () => ["invitations", "mine"],
  },
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listWorkspaces: mockListWorkspaces,
      listMyInvitations: mockListMyInvitations,
      googleLogin: vi.fn(),
    },
  };
});

import CallbackPage from "./page";

function renderCallback(locale: SupportedLocale = "en") {
  return render(
    <I18nProvider locale={locale} resources={RESOURCES}>
      <CallbackPage />
    </I18nProvider>,
  );
}

describe("CallbackPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset the source-backfill dismiss counter so a test that writes
    // it doesn't leak state into the next test (and the next test
    // doesn't inherit a cap-reached state from a previous run).
    for (let i = window.localStorage.length - 1; i >= 0; i--) {
      const k = window.localStorage.key(i);
      if (k && k.startsWith("multica.source_backfill.dismiss.")) {
        window.localStorage.removeItem(k);
      }
    }
    // Snapshot keys before deleting — forEach + delete skips entries because
    // the iteration index advances while the underlying list shrinks.
    Array.from(mockSearchParams.keys()).forEach((k) =>
      mockSearchParams.delete(k),
    );
    mockSearchParams.set("code", "test-code");
    mockLoginWithGoogle.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);
  });

  it("renders callback errors in the selected locale", async () => {
    mockSearchParams.delete("code");

    renderCallback("zh-Hans");

    expect(await screen.findByText("登录失败")).toBeInTheDocument();
    expect(screen.getByText("缺少授权码")).toBeInTheDocument();
    expect(screen.getByText("返回登录")).toBeInTheDocument();
  });

  it("shows access denied before checking for a missing authorization code", async () => {
    mockSearchParams.delete("code");
    mockSearchParams.set("error", "access_denied");

    renderCallback("zh-Hans");

    expect(await screen.findByText("访问被拒绝")).toBeInTheDocument();
    expect(screen.queryByText("缺少授权码")).not.toBeInTheDocument();
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("renders a localized generic failure instead of a raw English error", async () => {
    mockLoginWithGoogle.mockRejectedValue(
      new Error("upstream authentication failed"),
    );

    renderCallback("zh-Hans");

    expect(await screen.findByText("无法完成登录，请重试。")).toBeInTheDocument();
    expect(
      screen.queryByText("upstream authentication failed"),
    ).not.toBeInTheDocument();
  });

  it("localizes a stable login error code", async () => {
    mockLoginWithGoogle.mockRejectedValue(
      new ApiError("English fallback", 403, "Forbidden", {
        code: "signup_prohibited",
      }),
    );

    renderCallback("zh-Hans");

    expect(
      await screen.findByText("此自托管实例已禁止用户注册。"),
    ).toBeInTheDocument();
    expect(screen.queryByText("English fallback")).not.toBeInTheDocument();
  });

  it("preserves an actionable uncoded 4xx message from an older server", async () => {
    mockLoginWithGoogle.mockRejectedValue(
      new ApiError("legacy client error", 403, "Forbidden"),
    );

    renderCallback("zh-Hans");

    expect(await screen.findByText("legacy client error")).toBeInTheDocument();
  });

  it("retranslates a missing-email response without repeating the single-use code exchange", async () => {
    mockLoginWithGoogle.mockRejectedValue(
      new ApiError("Google did not provide an email address", 400, "Bad Request", {
        code: "google_account_no_email",
      }),
    );

    const view = renderCallback("zh-Hans");
    expect(
      await screen.findByText("Google 未提供本次登录所需的邮箱地址。"),
    ).toBeInTheDocument();

    view.rerender(
      <I18nProvider locale="ja" resources={RESOURCES}>
        <CallbackPage />
      </I18nProvider>,
    );

    expect(
      await screen.findByText("Googleから今回のログインに必要なメールアドレスが提供されませんでした。"),
    ).toBeInTheDocument();
    expect(mockLoginWithGoogle).toHaveBeenCalledTimes(1);
    expect(mockPush).not.toHaveBeenCalled();
  });

  // The response matrix lives in callback-error.test.ts; this covers page wiring.
  it("does not turn a provider failure into a user diagnosis even if it carries a known code", async () => {
    mockLoginWithGoogle.mockRejectedValue(
      new ApiError("internal provider detail", 502, "Bad Gateway", {
        code: "oauth_code_invalid",
      }),
    );

    renderCallback("zh-Hans");

    expect(await screen.findByText("无法完成登录，请重试。")).toBeInTheDocument();
    expect(screen.queryByText("internal provider detail")).not.toBeInTheDocument();
  });

  it.each([
    ["Desktop", "platform:desktop"],
    ["CLI", "cli_callback:http://127.0.0.1:46233/callback,cli_state:test"],
  ])("uses the same localized error handling for the %s callback", async (_flow, state) => {
    const { api: mockedApi } = await import("@multica/core/api");
    vi.mocked(mockedApi.googleLogin).mockRejectedValue(
      new ApiError("English fallback", 403, "Forbidden", { code: "signup_prohibited" }),
    );
    mockSearchParams.set("state", state);

    renderCallback("zh-Hans");

    expect(await screen.findByText("此自托管实例已禁止用户注册。")).toBeInTheDocument();
    expect(mockedApi.googleLogin).toHaveBeenCalledTimes(1);
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
    expect(mockPush).not.toHaveBeenCalled();
  });

  it("unonboarded user honors a safe next= (e.g. /invite/{id}) so invitees aren't trapped", async () => {
    mockSearchParams.set("state", "next:/invite/abc123");
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
    expect(mockPush).not.toHaveBeenCalledWith(paths.onboarding());
    // nextUrl is a fast path — listMyInvitations should not be queried.
    expect(mockListMyInvitations).not.toHaveBeenCalled();
  });

  it("unonboarded user with no next= and no pending invitations lands on /onboarding", async () => {
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
    expect(mockListMyInvitations).toHaveBeenCalled();
  });

  it("unonboarded user with pending invitations lands on /invitations", async () => {
    mockListMyInvitations.mockResolvedValue([
      {
        id: "inv-1",
        workspace_id: "ws-1",
        workspace_name: "Acme",
        role: "member",
        status: "pending",
      },
    ]);
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.invitations());
    });
    expect(mockPush).not.toHaveBeenCalledWith(paths.onboarding());
  });

  it("onboarded user with workspace lands in that workspace", async () => {
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockListWorkspaces.mockResolvedValue([
      {
        id: "ws-1",
        name: "Acme",
        slug: "acme",
        description: null,
        context: null,
        settings: {},
        repos: [],
        issue_prefix: "ACME",
        avatar_url: null,
        created_at: "",
        updated_at: "",
      },
    ]);
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").issues());
    });
    // Already-onboarded users skip the listMyInvitations check; new invites
    // surface in the sidebar instead of the wall.
    expect(mockListMyInvitations).not.toHaveBeenCalled();
  });

  it("onboarded user ignores unsafe next= targets and lands on the default destination", async () => {
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", "next:https://evil.example");

    renderCallback();

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalled();
    });
    expect(mockPush).not.toHaveBeenCalledWith("https://evil.example");
  });

  it("onboarded user honors a safe next= target (e.g. /invite/{id})", async () => {
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", "next:/invite/abc123");

    renderCallback();

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
  });

  it("falls through to /onboarding when listMyInvitations errors", async () => {
    mockListMyInvitations.mockRejectedValue(new Error("network"));
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("redirects to CLI callback with token when state contains valid cli_callback", async () => {
    const { api: mockedApi } = await import("@multica/core/api");
    const mockGoogleLogin = mockedApi.googleLogin as ReturnType<typeof vi.fn>;

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      mockSearchParams.set(
        "state",
        "cli_callback:http://127.0.0.1:46233/callback,cli_state:abc123",
      );
      mockGoogleLogin.mockResolvedValue({ token: "cli-jwt-token" });

      renderCallback();

      await waitFor(() => {
        expect(mockGoogleLogin).toHaveBeenCalledWith(
          "test-code",
          expect.stringContaining("/auth/callback"),
        );
      });

      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "http://127.0.0.1:46233/callback?token=cli-jwt-token&state=abc123",
        );
      });
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("falls through to normal web flow when state contains invalid cli_callback", async () => {
    mockSearchParams.set("state", "cli_callback:https://evil.com/callback");
    mockLoginWithGoogle.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);

    renderCallback();

    await waitFor(() => {
      // Normal web flow: loginWithGoogle is called (not googleLogin)
      expect(mockLoginWithGoogle).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("redirects to CLI callback even when state also contains platform:desktop", async () => {
    // cli_callback takes precedence over platform:desktop — the CLI flow
    // is a specific user intent that should not be derailed by desktop flag.
    const { api: mockedApi } = await import("@multica/core/api");
    const mockGoogleLogin = mockedApi.googleLogin as ReturnType<typeof vi.fn>;

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      mockSearchParams.set(
        "state",
        "platform:desktop,cli_callback:http://localhost:12345/callback,cli_state:mystate",
      );
      mockGoogleLogin.mockResolvedValue({ token: "mixed-jwt" });

      renderCallback();

      await waitFor(() => {
        expect(mockGoogleLogin).toHaveBeenCalled();
      });

      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "http://localhost:12345/callback?token=mixed-jwt&state=mystate",
        );
      });
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("onboarded users with missing source land in the workspace; the source-backfill modal is mounted there", async () => {
    // Source attribution backfill is now an in-workspace modal — see
    // `<SourceBackfillModal />` mounted inside `DashboardLayout`. The
    // callback page is intentionally agnostic about it.
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({
        onboarded_at: "2026-01-01T00:00:00Z",
        onboarding_questionnaire: {},
      }),
    );
    mockListWorkspaces.mockResolvedValue([
      {
        id: "ws-1",
        name: "Acme",
        slug: "acme",
        description: null,
        context: null,
        settings: {},
        repos: [],
        issue_prefix: "ACME",
        created_at: "",
        updated_at: "",
      },
    ]);
    renderCallback();
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").issues());
    });
  });
});
