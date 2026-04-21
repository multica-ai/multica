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
    mockLoginWithGoogle.mockResolvedValue(undefined);
    mockListWorkspaces.mockResolvedValue([]);
  });

  it("falls back to paths.newWorkspace() when no next= is present and the user has no workspace", async () => {
    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.newWorkspace());
    });
  });

  it("ignores unsafe next= targets from the OAuth state and still lands on the default destination", async () => {
    mockSearchParams.set("state", "next:https://evil.example");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.newWorkspace());
    });
    expect(mockPush).not.toHaveBeenCalledWith("https://evil.example");
  });

  it("honors a safe next= target (e.g. /invite/{id})", async () => {
    mockSearchParams.set("state", "next:/invite/abc123");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
  });
});
