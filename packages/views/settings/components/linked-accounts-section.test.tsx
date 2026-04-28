import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const apiMock = vi.hoisted(() => ({
  listNotificationBindings: vi.fn(),
  deleteNotificationBinding: vi.fn(),
  startDingTalkBinding: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: apiMock,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { LinkedAccountsSection } from "./linked-accounts-section";

describe("LinkedAccountsSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listNotificationBindings.mockResolvedValue({ bindings: [] });
    apiMock.deleteNotificationBinding.mockResolvedValue(undefined);
    apiMock.startDingTalkBinding.mockResolvedValue({
      auth_url: "https://login.dingtalk.com/oauth2/auth?state=test",
    });
  });

  it("starts dingtalk binding when connect is clicked", async () => {
    const user = userEvent.setup();

    render(<LinkedAccountsSection />);

    await user.click(
      await screen.findByRole("button", { name: "Connect" }),
    );

    await waitFor(() => {
      expect(apiMock.startDingTalkBinding).toHaveBeenCalledWith({
        next_path: window.location.pathname,
      });
    });
  });

  it("shows the connected dingtalk account details", async () => {
    apiMock.listNotificationBindings.mockResolvedValue({
      bindings: [
        {
          id: "binding-1",
          provider: "dingtalk",
          external_user_id: "union-id-123",
          display_name: "Ding User",
          status: "active",
          created_at: "2026-04-27T00:00:00Z",
          updated_at: "2026-04-27T00:00:00Z",
        },
      ],
    });

    render(<LinkedAccountsSection />);

    expect(await screen.findByText("Ding User")).toBeInTheDocument();
    expect(screen.getByText("External user: union-id-123")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Disconnect" })).toBeInTheDocument();
  });
});
