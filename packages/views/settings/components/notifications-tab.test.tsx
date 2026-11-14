import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithI18n } from "../../test/i18n";
import { ApiError } from "@multica/core/api";

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
        "Google Chat space → Apps & integrations → Webhooks → Add webhook → copy URL. Saving sends a test message to the space.",
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

    expect(screen.getByText("Connected to space AAAA…")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Replace" }));
    expect(
      screen.getByRole("textbox", { name: "Google Chat webhook URL" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(state.mutateAsync).not.toHaveBeenCalled();
    const dialog = screen.getByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
    await waitFor(() => {
      expect(state.mutateAsync).toHaveBeenCalledWith({
        comments: "muted",
        google_chat_webhook: "",
      });
    });
  });

  it("keeps input visible with a connecting label while confirmation is pending", () => {
    state.isPending = true;
    renderWithI18n(<NotificationsTab />);
    expect(screen.getByRole("textbox", { name: "Google Chat webhook URL" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Connecting…" })).toBeDisabled();
    expect(screen.queryByText(/Connected to space/)).not.toBeInTheDocument();
  });

  it.each([
    ["google_chat_invalid_url", "Saisissez une URL de webhook Google Chat commençant par"],
    ["google_chat_missing_credentials", "L'URL du webhook doit inclure key et token."],
    ["google_chat_connection_failed", "Impossible de se connecter à Google Chat."],
    ["unknown", "Échec de la mise à jour"],
  ])("shows localized inline errors for %s and preserves input", async (code, expected) => {
    state.mutateAsync.mockRejectedValue(new ApiError("raw English server message", 400, "Bad Request", { code }));
    renderWithI18n(<NotificationsTab />, { locale: "fr" });
    const input = screen.getByRole("textbox", { name: "URL du webhook Google Chat" });
    fireEvent.change(input, { target: { value: "https://example.com" } });
    fireEvent.submit(input.closest("form")!);
    const error = await screen.findByRole("alert");
    expect(error.textContent).toContain(expected);
    expect(error.textContent).not.toContain("raw English");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveValue("https://example.com");
    fireEvent.change(input, { target: { value: "https://chat.googleapis.com" } });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("notification setting hierarchy", () => {
  beforeEach(() => {
    state.isPending = false;
  });
  it.each([
    { systemMuted: false, chatMuted: false },
    { systemMuted: false, chatMuted: true },
    { systemMuted: true, chatMuted: false },
    { systemMuted: true, chatMuted: true },
  ])("nests chat replies under system notifications: %j", ({ systemMuted, chatMuted }) => {
    state.preferences = {
      ...(systemMuted ? { system_notifications: "muted" } : {}),
      ...(chatMuted ? { chat_replies: "muted" } : {}),
    };
    renderWithI18n(<NotificationsTab />);
    expect(screen.queryByRole("region", { name: "Chat" })).toBeNull();
    const system = screen.getByRole("region", { name: "System Notifications" });
    if (systemMuted) {
      expect(within(system).queryByRole("switch", { name: "Include chat replies" })).toBeNull();
      expect(within(system).getAllByRole("switch")).toHaveLength(1);
    } else {
      const replies = within(system).getByRole("switch", { name: "Include chat replies" });
      if (chatMuted) expect(replies).not.toBeChecked();
      else expect(replies).toBeChecked();
      expect(within(system).getAllByRole("switch")).toHaveLength(2);
    }
    expect(within(system).queryByText("When an agent finishes replying in a chat.")).toBeNull();
    expect(within(system).queryByText("Enable system notifications first.")).toBeNull();
    expect(within(system).queryByText("Applies to Inbox notifications and chat replies.")).toBeNull();
  });
});
