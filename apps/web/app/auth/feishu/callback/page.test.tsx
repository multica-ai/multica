import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";

const { mockPush, mockSearchParams, mockLoginWithFeishu, mockListWorkspaces } =
  vi.hoisted(() => ({
    mockPush: vi.fn(),
    mockSearchParams: new URLSearchParams(),
    mockLoginWithFeishu: vi.fn(),
    mockListWorkspaces: vi.fn(),
  }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => mockSearchParams,
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: vi.fn() }),
}));

vi.mock("@multica/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/auth")>(
      "@multica/core/auth",
    );
  return {
    ...actual,
    useAuthStore: (selector: (s: unknown) => unknown) =>
      selector({ loginWithFeishu: mockLoginWithFeishu }),
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    feishuLogin: vi.fn(),
  },
}));

import CallbackPage from "./page";

describe("Feishu CallbackPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSearchParams.forEach((_v, k) => mockSearchParams.delete(k));
    mockSearchParams.set("code", "test-code");
    mockLoginWithFeishu.mockResolvedValue(undefined);
    mockListWorkspaces.mockResolvedValue([]);
  });

  it("falls back to paths.newWorkspace() when no next= and user has no workspace", async () => {
    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.newWorkspace());
    });
  });

  it("ignores unsafe next= targets from the OAuth state", async () => {
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
