import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mockUpdateMyPreferences = vi.hoisted(() => vi.fn());
const mockSetUser = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({
      user: {
        id: "user-1",
        name: "Test User",
        email: "test@example.com",
        avatar_url: null,
        preferences: {},
        onboarded_at: null,
        onboarding_questionnaire: {},
        starter_content_state: "imported",
        language: null,
        timezone: null,
        profile_description: "",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      },
      setUser: mockSetUser,
    }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    updateMyPreferences: mockUpdateMyPreferences,
  },
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => false,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab", () => {
  it("shows a comments setting with Inbox enabled by default", () => {
    render(<NotificationsTab />);

    expect(
      screen.getByRole("heading", { name: "Comments" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("New comments on issues you follow."),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("switch", { name: "Send comments to Inbox" }),
    ).toBeChecked();
  });

  it("stores the comments setting under the Inbox notification route", async () => {
    const user = userEvent.setup();
    mockUpdateMyPreferences.mockResolvedValue({
      id: "user-1",
      preferences: { notifications: { inbox: { new_comment: "off" } } },
    });

    render(<NotificationsTab />);

    await user.click(
      screen.getByRole("switch", { name: "Send comments to Inbox" }),
    );

    expect(mockUpdateMyPreferences).toHaveBeenCalledWith({
      notifications: {
        inbox: { new_comment: "off" },
      },
    });
    expect(mockSetUser).toHaveBeenCalledWith({
      id: "user-1",
      preferences: { notifications: { inbox: { new_comment: "off" } } },
    });
  });
});
