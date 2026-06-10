import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";

const {
  mockPush,
  mockSearchParams,
  mockLoginWithGoogle,
  mockLoginWithDingTalk,
  mockListWorkspaces,
  mockGoogleLogin,
  mockDingtalkLogin,
  mockCompleteDingTalkBinding,
  mockCompleteGoogleBinding,
  mockListMyInvitations,
  mockSetQueryData,
} = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchParams: new URLSearchParams(),
  mockLoginWithGoogle: vi.fn(),
  mockLoginWithDingTalk: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockGoogleLogin: vi.fn(),
  mockDingtalkLogin: vi.fn(),
  mockCompleteDingTalkBinding: vi.fn(),
  mockCompleteGoogleBinding: vi.fn(),
  mockListMyInvitations: vi.fn(),
  mockSetQueryData: vi.fn(),
}));

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
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => mockSearchParams,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: mockSetQueryData }),
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
      selector({ loginWithGoogle: mockLoginWithGoogle, loginWithDingTalk: mockLoginWithDingTalk }),
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: {
    list: () => ["workspaces"],
    myInvitations: () => ["invitations", "mine"],
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    googleLogin: mockGoogleLogin,
    dingtalkLogin: mockDingtalkLogin,
    completeDingTalkBinding: mockCompleteDingTalkBinding,
    completeGoogleBinding: mockCompleteGoogleBinding,
    listMyInvitations: mockListMyInvitations,
  },
}));

import CallbackPage from "./page";

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
    mockLoginWithDingTalk.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
    mockCompleteDingTalkBinding.mockResolvedValue({
      binding: {
        id: "binding-1",
        provider: "dingtalk",
        external_user_id: "ding-open-id",
        display_name: "Ding User",
        status: "active",
        metadata: {},
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      next_path: "/acme/settings",
    });
    mockCompleteGoogleBinding.mockResolvedValue({
      binding: {
        id: "binding-google",
        provider: "google",
        external_user_id: "google-user-id",
        display_name: "Google User",
        status: "active",
        metadata: {},
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      next_path: "/acme/settings",
    });
    mockListMyInvitations.mockResolvedValue([]);
  });

  it("unonboarded user honors a safe next= (e.g. /invite/{id}) so invitees aren't trapped", async () => {
    mockSearchParams.set("state", "next:/invite/abc123");
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
    expect(mockPush).not.toHaveBeenCalledWith(paths.onboarding());
    // nextUrl is a fast path — listMyInvitations should not be queried.
    expect(mockListMyInvitations).not.toHaveBeenCalled();
  });

  it("unonboarded user with no next= and no pending invitations lands on /onboarding", async () => {
    render(<CallbackPage />);
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
    render(<CallbackPage />);
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
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").root());
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

    render(<CallbackPage />);

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

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
  });

  // DingTalk login tests (provider:dingtalk in state)
  it("routes to loginWithDingTalk when state contains provider:dingtalk", async () => {
    mockLoginWithDingTalk.mockResolvedValue(makeUser());
    mockSearchParams.set("state", "provider:dingtalk");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockLoginWithDingTalk).toHaveBeenCalledWith(
        "test-code",
        expect.stringContaining("/auth/callback"),
      );
    });
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("redirects DingTalk CLI OAuth callbacks to the local CLI callback", async () => {
    const cliPart = `cli:${encodeURIComponent(
      new URLSearchParams({
        callback: "http://localhost:65202/callback",
        state: "state-123",
      }).toString(),
    )}`;
    mockSearchParams.set("state", `provider:dingtalk,${cliPart}`);
    mockDingtalkLogin.mockResolvedValue({
      token: "cli-jwt",
      user: makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    });

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: {
        ...originalLocation,
        origin: "http://localhost:3000",
        set href(value: string) {
          hrefSetter(value);
        },
      },
    });

    try {
      render(<CallbackPage />);

      await waitFor(() => {
        expect(mockDingtalkLogin).toHaveBeenCalledWith(
          "test-code",
          "http://localhost:3000/auth/callback",
        );
      });
      expect(hrefSetter).toHaveBeenCalledWith(
        "http://localhost:65202/callback?token=cli-jwt&state=state-123",
      );
      expect(mockLoginWithDingTalk).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("redirects Google CLI OAuth callbacks to the local CLI callback", async () => {
    const cliPart = `cli:${encodeURIComponent(
      new URLSearchParams({
        callback: "http://127.0.0.1:65202/callback",
        state: "google-state",
      }).toString(),
    )}`;
    mockSearchParams.set("state", cliPart);
    mockGoogleLogin.mockResolvedValue({
      token: "google-cli-jwt",
      user: makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    });

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: {
        ...originalLocation,
        origin: "http://localhost:3000",
        set href(value: string) {
          hrefSetter(value);
        },
      },
    });

    try {
      render(<CallbackPage />);

      await waitFor(() => {
        expect(mockGoogleLogin).toHaveBeenCalledWith(
          "test-code",
          "http://localhost:3000/auth/callback",
        );
      });
      expect(hrefSetter).toHaveBeenCalledWith(
        "http://127.0.0.1:65202/callback?token=google-cli-jwt&state=google-state",
      );
      expect(mockLoginWithGoogle).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("unonboarded DingTalk user lands on /onboarding", async () => {
    mockLoginWithDingTalk.mockResolvedValue(makeUser());
    mockSearchParams.set("state", "provider:dingtalk");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("falls through to /onboarding when listMyInvitations errors", async () => {
    mockListMyInvitations.mockRejectedValue(new Error("network"));
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });
  it("onboarded DingTalk user honors safe next= from state", async () => {
    mockLoginWithDingTalk.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", "provider:dingtalk,next:/invite/xyz");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/xyz");
    });
  });

  // DingTalk binding tests (dingtalk. prefix in state — from OPE-20 notification flow)
  it("routes dingtalk callback through the binding completion API", async () => {
    mockSearchParams.set("state", "dingtalk.signed-state");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockCompleteDingTalkBinding).toHaveBeenCalledWith(
        "test-code",
        "dingtalk.signed-state",
      );
      expect(mockPush).toHaveBeenCalledWith("/acme/settings");
    });
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("falls back to / when dingtalk callback has no next_path", async () => {
    mockSearchParams.set("state", "dingtalk.signed-state");
    mockCompleteDingTalkBinding.mockResolvedValue({
      binding: {
        id: "binding-1",
        provider: "dingtalk",
        external_user_id: "ding-open-id",
        display_name: "Ding User",
        status: "active",
        metadata: {},
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      next_path: null,
    });

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.root());
    });
  });

  it("routes google binding callbacks through the state-based completion API", async () => {
    mockSearchParams.set("state", "google.signed-state");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockCompleteGoogleBinding).toHaveBeenCalledWith(
        "test-code",
        "google.signed-state",
      );
      expect(mockPush).toHaveBeenCalledWith("/acme/settings");
    });
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
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
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").issues());
    });
  });
});
