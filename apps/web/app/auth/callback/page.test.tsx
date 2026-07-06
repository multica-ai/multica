import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";

const {
  mockPush,
  mockSearchParams,
  mockLoginWithGoogle,
  mockListWorkspaces,
  mockListMyInvitations,
  mockSetQueryData,
} = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchParams: new URLSearchParams(),
  mockLoginWithGoogle: vi.fn(),
  mockListWorkspaces: vi.fn(),
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
      selector({ loginWithGoogle: mockLoginWithGoogle }),
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
    listMyInvitations: mockListMyInvitations,
    googleLogin: vi.fn(),
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
    mockListWorkspaces.mockResolvedValue([]);
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

  it("falls through to /onboarding when listMyInvitations errors", async () => {
    mockListMyInvitations.mockRejectedValue(new Error("network"));
    render(<CallbackPage />);
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

      render(<CallbackPage />);

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

    render(<CallbackPage />);

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

      render(<CallbackPage />);

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
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").issues());
    });
  });
});
