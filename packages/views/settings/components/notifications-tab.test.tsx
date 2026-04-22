import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const {
  mockUpdateWorkspace,
  mockSetQueryData,
  mockToastSuccess,
  mockToastError,
} = vi.hoisted(() => ({
  mockUpdateWorkspace: vi.fn(),
  mockSetQueryData: vi.fn(),
  mockToastSuccess: vi.fn(),
  mockToastError: vi.fn(),
}));

const workspace = {
  id: "ws-1",
  name: "Workspace",
  slug: "workspace",
  description: null,
  context: null,
  settings: {
    existing: "kept",
    telegram: {
      bot_token: "old-token",
      user_id: "old-user",
    },
  },
  repos: [],
  issue_prefix: "WS",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspace,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members", "ws-1"] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updateWorkspace: (...args: unknown[]) => mockUpdateWorkspace(...args),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: [{ user_id: "user-1", role: "owner" }],
  }),
  useQueryClient: () => ({
    setQueryData: mockSetQueryData,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateWorkspace.mockResolvedValue(workspace);
  });

  it("merges Telegram settings into existing workspace settings on save", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />);

    const botTokenInput = screen.getByPlaceholderText("123456:ABCDEF...");
    const userIdInput = screen.getByPlaceholderText("123456789");

    await user.clear(botTokenInput);
    await user.type(botTokenInput, "new-token");
    await user.clear(userIdInput);
    await user.type(userIdInput, "new-user");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(mockUpdateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        existing: "kept",
        telegram: {
          bot_token: "new-token",
          user_id: "new-user",
        },
      },
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Telegram notifications saved");
    expect(mockSetQueryData).toHaveBeenCalled();
  });
});
