import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";
import { encodeOAuthState } from "@multica/core/auth";

const stateFor = (parts: {
  providerId?: string;
  platform?: string;
  next?: string;
  nonce?: string;
}) =>
  encodeOAuthState({
    nonce: "n1",
    ...parts,
  });

const {
  mockPush,
  mockSearchParams,
  mockLoginWithOAuth,
  mockListWorkspaces,
  mockOAuthLogin,
  mockOAuthProviders,
} = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchParams: new URLSearchParams(),
  mockLoginWithOAuth: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockOAuthLogin: vi.fn(),
  mockOAuthProviders: {
    current: {} as Record<
      string,
      { clientId: string; authorizeUrl: string; callbackPath: string; scope: string }
    >,
  },
}));

const makeUser = (overrides: Partial<{ onboarded_at: string | null }> = {}) => ({
  id: "user-1",
  name: "Test",
  email: "test@multica.ai",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

vi.mock("../navigation/context", () => ({
  useNavigation: () => ({
    push: mockPush,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/auth/callback",
    searchParams: mockSearchParams,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: vi.fn() }),
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
      selector({
        loginWithOAuth: mockLoginWithOAuth,
      }),
  };
});

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (s: unknown) => unknown) =>
    selector({ oauthProviders: mockOAuthProviders.current }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    oauthLogin: mockOAuthLogin,
  },
}));

import { OAuthCallbackPage } from "./oauth-callback-page";

describe("OAuthCallbackPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    [...mockSearchParams.keys()].forEach((k) => mockSearchParams.delete(k));
    mockSearchParams.set("code", "test-code");
    mockLoginWithOAuth.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
    mockOAuthProviders.current = {
      google: {
        clientId: "google-client",
        authorizeUrl: "https://accounts.google.com/o/oauth2/v2/auth",
        callbackPath: "/auth/callback",
        scope: "openid email profile",
      },
      github: {
        clientId: "github-client",
        authorizeUrl: "https://github.com/login/oauth/authorize",
        callbackPath: "/auth/callback",
        scope: "read:user user:email",
      },
    };
  });

  it("unonboarded user lands on /onboarding regardless of next=", async () => {
    mockSearchParams.set("state", stateFor({ providerId: "google", next: "/invite/abc123" }));
    render(<OAuthCallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
    expect(mockPush).not.toHaveBeenCalledWith("/invite/abc123");
  });

  it("unonboarded user with no next= also lands on /onboarding", async () => {
    mockSearchParams.set("state", stateFor({ providerId: "google" }));
    render(<OAuthCallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("onboarded user ignores unsafe next= targets and lands on the default destination", async () => {
    mockLoginWithOAuth.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", stateFor({ providerId: "google", next: "https://evil.example" }));

    render(<OAuthCallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalled();
    });
    expect(mockPush).not.toHaveBeenCalledWith("https://evil.example");
  });

  it("onboarded user honors a safe next= target (e.g. /invite/{id})", async () => {
    mockLoginWithOAuth.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", stateFor({ providerId: "google", next: "/invite/abc123" }));

    render(<OAuthCallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
  });

  it("dispatches to loginWithOAuth with the provider id and nonce from state", async () => {
    mockLoginWithOAuth.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    mockSearchParams.set("state", stateFor({ providerId: "github", nonce: "abc123" }));

    render(<OAuthCallbackPage />);

    await waitFor(() => {
      expect(mockLoginWithOAuth).toHaveBeenCalled();
    });
    const [providerId, , , nonce] = mockLoginWithOAuth.mock.calls[0]!;
    expect(providerId).toBe("github");
    expect(nonce).toBe("abc123");
  });

  it("errors out when state has no provider token — no implicit default", async () => {
    render(<OAuthCallbackPage />);

    await waitFor(() => {
      expect(mockPush).not.toHaveBeenCalled();
    });
    expect(mockLoginWithOAuth).not.toHaveBeenCalled();
    expect(mockOAuthLogin).not.toHaveBeenCalled();
  });

  it("errors out when provider token is unknown to the server", async () => {
    mockSearchParams.set("state", stateFor({ providerId: "mystery" }));

    render(<OAuthCallbackPage />);

    await new Promise((r) => setTimeout(r, 10));
    expect(mockLoginWithOAuth).not.toHaveBeenCalled();
    expect(mockOAuthLogin).not.toHaveBeenCalled();
  });

  it("errors out when the state-named provider is not configured on this instance", async () => {
    mockOAuthProviders.current = {
      github: {
        clientId: "github-client",
        authorizeUrl: "https://github.com/login/oauth/authorize",
        callbackPath: "/auth/callback",
        scope: "read:user user:email",
      },
    };
    mockSearchParams.set("state", stateFor({ providerId: "google" }));

    render(<OAuthCallbackPage />);

    await new Promise((r) => setTimeout(r, 10));
    expect(mockLoginWithOAuth).not.toHaveBeenCalled();
    expect(mockOAuthLogin).not.toHaveBeenCalled();
  });

  it("does not dispatch before the provider config has hydrated", async () => {
    mockOAuthProviders.current = {};
    mockSearchParams.set("state", stateFor({ providerId: "github" }));

    render(<OAuthCallbackPage />);

    await new Promise((r) => setTimeout(r, 10));
    expect(mockLoginWithOAuth).not.toHaveBeenCalled();
    expect(mockOAuthLogin).not.toHaveBeenCalled();
  });

  it("rejects callbacks with no nonce in state (CSRF protection)", async () => {
    mockSearchParams.set("state", "provider=github");

    render(<OAuthCallbackPage />);

    await new Promise((r) => setTimeout(r, 10));
    expect(mockLoginWithOAuth).not.toHaveBeenCalled();
    expect(mockOAuthLogin).not.toHaveBeenCalled();
  });

  it("desktop flow uses api.oauthLogin with the provider id", async () => {
    mockOAuthLogin.mockResolvedValue({ token: "tok", user: makeUser() });
    mockSearchParams.set("state", stateFor({ providerId: "github", platform: "desktop" }));

    render(<OAuthCallbackPage />);

    await waitFor(() => {
      expect(mockOAuthLogin).toHaveBeenCalled();
    });
    expect(mockOAuthLogin.mock.calls[0]![0]).toBe("github");
  });
});
