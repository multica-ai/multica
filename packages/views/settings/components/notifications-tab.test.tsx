import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  preferences: {} as Record<string, string>,
  isPending: false,
  mutate: vi.fn(),
  mutateAsync: vi.fn(async () => ({
    workspace_id: "workspace-1",
    preferences: {},
  })),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({
    data: {
      workspace_id: "workspace-1",
      preferences: state.preferences,
    },
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({
    id: "workspace-1",
    name: "Acme",
    avatar_url: null,
  }),
}));

vi.mock("@multica/core/notification-preferences/queries", () => ({
  notificationPreferenceOptions: () => ({}),
}));

vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({
    mutate: state.mutate,
    mutateAsync: state.mutateAsync,
    isPending: state.isPending,
  }),
}));

vi.mock("./browser-notification-setting", () => ({
  BrowserNotificationSetting: () => null,
}));

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab Google Chat settings", () => {
  beforeEach(() => {
    state.preferences = {};
    state.isPending = false;
    state.mutate.mockReset();
    state.mutateAsync.mockReset();
    state.mutateAsync.mockResolvedValue({
      workspace_id: "workspace-1",
      preferences: {},
    });
  });

  it("saves a webhook from the unset state", async () => {
    renderWithI18n(<NotificationsTab />);

    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    expect(
      screen.getByText(
        "Google Chat space → Apps & integrations → Webhooks → Add webhook → copy URL.",
      ),
    ).toBeInTheDocument();

    const webhook =
      "https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t";
    fireEvent.change(
      screen.getByRole("textbox", { name: "Google Chat webhook URL" }),
      { target: { value: webhook } },
    );
    fireEvent.click(save);

    await waitFor(() => {
      expect(state.mutateAsync).toHaveBeenCalledWith({
        google_chat_webhook: webhook,
      });
    });
  });

  it("shows a masked configured value with replace and remove actions", async () => {
    state.preferences = {
      comments: "muted",
      google_chat_webhook:
        "https://chat.googleapis.com/v1/spaces/AAAA…/messages?key=••••&token=••••",
    };
    renderWithI18n(<NotificationsTab />);

    expect(screen.getByText(/spaces\/AAAA…\/messages/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(
      screen.getByRole("textbox", { name: "Google Chat webhook URL" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => {
      expect(state.mutateAsync).toHaveBeenCalledWith({
        comments: "muted",
        google_chat_webhook: "",
      });
    });
  });
});
