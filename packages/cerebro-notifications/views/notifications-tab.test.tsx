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
  it("shows separate New comment choices for assigned and followed issues", () => {
    render(<NotificationsTab />);

    expect(
      screen.getByRole("switch", {
        name: "Inbox: On issues you're assigned to: New comment",
      }),
    ).toBeChecked();
    expect(
      screen.getByRole("switch", {
        name: "Inbox: On issues you follow: New comment",
      }),
    ).toBeChecked();
  });

  it("stores the assigned comment setting under its split routing key", async () => {
    const user = userEvent.setup();
    mockUpdateMyPreferences.mockResolvedValue({
      id: "user-1",
      preferences: {
        notifications: { inbox: { "new_comment.assignee": "off" } },
      },
    });

    render(<NotificationsTab />);

    await user.click(
      screen.getByRole("switch", {
        name: "Inbox: On issues you're assigned to: New comment",
      }),
    );

    expect(mockUpdateMyPreferences).toHaveBeenCalledWith({
      notifications: {
        inbox: { "new_comment.assignee": "off" },
      },
    });
    expect(mockSetUser).toHaveBeenCalledWith({
      id: "user-1",
      preferences: {
        notifications: { inbox: { "new_comment.assignee": "off" } },
      },
    });
  });
});
