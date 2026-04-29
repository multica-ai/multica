import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";

const { mockPush, mockSearchParams, mockLoginWithGoogle, mockListWorkspaces } =
  vi.hoisted(() => ({
    mockPush: vi.fn(),
    mockSearchParams: new URLSearchParams(),
    mockLoginWithGoogle: vi.fn(),
    mockListWorkspaces: vi.fn(),
  }));

const makeUser = (overrides: Partial<{ onboarded_at: string | null }> = {}) => ({
  id: "user-1",
  name: "Test",
  email: "test@multica.ai",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null as string | null,
  account_status: "active",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => mockSearchParams,
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
      selector({ loginWithGoogle: mockLoginWithGoogle }),
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    googleLogin: vi.fn(),
  },
}));

import CallbackPage from "./page";

describe("CallbackPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSearchParams.forEach((_v, k) => mockSearchParams.delete(k));
    mockSearchParams.set("code", "test-code");
    mockLoginWithGoogle.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
  });

  it("unonboarded user lands on /onboarding regardless of next=", async () => {
    mockSearchParams.set("state", "next:/invite/abc123");
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
    expect(mockPush).not.toHaveBeenCalledWith("/invite/abc123");
  });

  it("unonboarded user with no next= also lands on /onboarding", async () => {
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
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
});
