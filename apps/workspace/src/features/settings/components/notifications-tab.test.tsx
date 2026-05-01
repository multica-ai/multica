import React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { NotificationPreference } from "@/shared/types";
import { NotificationsTab } from "./notifications-tab";

const apiMocks = vi.hoisted(() => ({
  getNotificationPreferences: vi.fn(),
  updateNotificationPreferences: vi.fn(),
  testNotificationPreference: vi.fn(),
}));

vi.mock("@/shared/api", () => ({
  api: apiMocks,
}));

vi.mock("@/features/auth/queries", () => ({
  hasStoredSessionToken: () => true,
}));

function makePreference(overrides: Partial<NotificationPreference> = {}): NotificationPreference {
  return { ntfy_url: "", ntfy_token: "", disabled_types: [], ...overrides };
}

function renderWithQuery(ui: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

describe("NotificationsTab", () => {
  beforeEach(() => vi.clearAllMocks());

  it("renders with empty state and shows configure banner", async () => {
    apiMocks.getNotificationPreferences.mockResolvedValue(makePreference());
    renderWithQuery(<NotificationsTab />);
    await waitFor(() => {
      expect(screen.getByText(/configure an ntfy url/i)).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/ntfy topic url/i)).toHaveValue("");
  });

  it("shows active banner when ntfy_url is set", async () => {
    apiMocks.getNotificationPreferences.mockResolvedValue(
      makePreference({ ntfy_url: "https://ntfy.sh/my-topic" }),
    );
    renderWithQuery(<NotificationsTab />);
    await waitFor(() => {
      expect(screen.getByText(/ntfy push notifications active/i)).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/ntfy topic url/i)).toHaveValue("https://ntfy.sh/my-topic");
  });

  it("calls updateNotificationPreferences on save", async () => {
    const user = userEvent.setup();
    apiMocks.getNotificationPreferences.mockResolvedValue(makePreference());
    apiMocks.updateNotificationPreferences.mockResolvedValue(
      makePreference({ ntfy_url: "https://ntfy.sh/test" }),
    );
    renderWithQuery(<NotificationsTab />);
    await waitFor(() => screen.getByLabelText(/ntfy topic url/i));

    await user.clear(screen.getByLabelText(/ntfy topic url/i));
    await user.type(screen.getByLabelText(/ntfy topic url/i), "https://ntfy.sh/test");
    await user.click(screen.getByRole("button", { name: /save preferences/i }));

    await waitFor(() => {
      expect(apiMocks.updateNotificationPreferences).toHaveBeenCalledWith(
        expect.objectContaining({ ntfy_url: "https://ntfy.sh/test" }),
      );
    });
  });

  it("calls testNotificationPreference on test button click", async () => {
    const user = userEvent.setup();
    apiMocks.getNotificationPreferences.mockResolvedValue(
      makePreference({ ntfy_url: "https://ntfy.sh/my-topic" }),
    );
    apiMocks.testNotificationPreference.mockResolvedValue(undefined);
    renderWithQuery(<NotificationsTab />);

    await waitFor(() =>
      expect(screen.getByLabelText(/ntfy topic url/i)).toHaveValue("https://ntfy.sh/my-topic"),
    );
    await user.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => {
      expect(apiMocks.testNotificationPreference).toHaveBeenCalledWith(
        expect.objectContaining({ ntfy_url: "https://ntfy.sh/my-topic" }),
      );
    });
  });

  it("disables test button when ntfy_url is empty", async () => {
    apiMocks.getNotificationPreferences.mockResolvedValue(makePreference());
    renderWithQuery(<NotificationsTab />);
    await waitFor(() => screen.getByRole("button", { name: /test/i }));
    expect(screen.getByRole("button", { name: /test/i })).toBeDisabled();
  });

  it("toggling a notification type updates disabled_types on save", async () => {
    const user = userEvent.setup();
    apiMocks.getNotificationPreferences.mockResolvedValue(makePreference());
    apiMocks.updateNotificationPreferences.mockResolvedValue(
      makePreference({ disabled_types: ["new_comment"] }),
    );
    renderWithQuery(<NotificationsTab />);

    await waitFor(() =>
      expect(screen.getByRole("switch", { name: /new comment/i })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("switch", { name: /new comment/i }));
    await user.click(screen.getByRole("button", { name: /save preferences/i }));

    await waitFor(() => {
      expect(apiMocks.updateNotificationPreferences).toHaveBeenCalledWith(
        expect.objectContaining({ disabled_types: expect.arrayContaining(["new_comment"]) }),
      );
    });
  });
});
