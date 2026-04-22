import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const apiMock = vi.hoisted(() => ({
  listNotificationBindings: vi.fn(),
  listNotificationPreferences: vi.fn(),
  updateNotificationPreference: vi.fn(),
  deleteNotificationBinding: vi.fn(),
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

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listNotificationBindings.mockResolvedValue({ bindings: [] });
    apiMock.listNotificationPreferences.mockResolvedValue({
      preferences: [
        {
          channel: "inbox",
          event_type: "mentioned",
          enabled: true,
          binding_id: null,
          requires_binding: false,
        },
        {
          channel: "dingtalk",
          event_type: "mentioned",
          enabled: false,
          binding_id: null,
          requires_binding: true,
        },
      ],
    });
    apiMock.updateNotificationPreference.mockImplementation(async (payload) => ({
      ...payload,
      binding_id: null,
      requires_binding: payload.channel === "dingtalk",
    }));
    apiMock.deleteNotificationBinding.mockResolvedValue(undefined);
  });

  it("shows not connected state for dingtalk and keeps the toggle disabled", async () => {
    render(<NotificationsTab />);

    expect(await screen.findByText("When you are mentioned")).toBeInTheDocument();
    expect(screen.getByText("No DingTalk account connected")).toBeInTheDocument();

    const switches = screen.getAllByRole("switch");
    expect(switches).toHaveLength(2);
    expect(switches[1]).toHaveAttribute("aria-disabled", "true");
  });

  it("updates the inbox preference when the switch is toggled", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />);

    const inboxSwitch = await screen.findByRole("switch", {
      name: "Toggle Inbox mentions",
    });
    expect(inboxSwitch).toHaveAttribute("aria-checked", "true");

    await user.click(inboxSwitch);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
        channel: "inbox",
        event_type: "mentioned",
        enabled: false,
      });
    });
  });
});
