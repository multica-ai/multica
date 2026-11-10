import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { applyNotificationPreferencePatch } from "@multica/core/notification-preferences/patch";
import type { NotificationPreferences } from "@multica/core/types/notification-preference";
import { renderWithI18n } from "../../test/i18n";
import { NotificationsTab } from "./notifications-tab";

vi.mock("@multica/core/api", () => ({
  api: { getNotificationPreferences: vi.fn(), updateNotificationPreferences: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useRequiredWorkspaceSlug: () => "acme",
  useCurrentWorkspace: () => ({ id: "ws", name: "Acme" }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));

const get = vi.mocked(api.getNotificationPreferences);
const update = vi.mocked(api.updateNotificationPreferences);
let stored: NotificationPreferences = {};

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NotificationsTab />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  stored = {};
  get.mockReset().mockImplementation(async () => ({ workspace_id: "ws", preferences: { ...stored } }));
  update.mockReset().mockImplementation(async (patch) => {
    stored = applyNotificationPreferencePatch(stored, patch);
    return { workspace_id: "ws", preferences: { ...stored } };
  });
});

it("switches to Focused, hides the group switches and saves a focused filter", async () => {
  const user = userEvent.setup();
  mount();

  expect(await screen.findByRole("switch", { name: "Assignments" })).toBeVisible();
  expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
  expect(screen.getByText("Applies to new notifications only and doesn't change your subscriptions.")).toBeVisible();

  const mode = screen.getByRole("group", { name: "Inbox mode" });
  expect(screen.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
  await user.click(screen.getByRole("button", { name: "Focused" }));
  await waitFor(() => expect(update).toHaveBeenCalledWith({ inbox_mode: "focused" }, "acme"));
  expect(mode).toContainElement(screen.getByRole("button", { name: "Focused" }));

  const blocked = await screen.findByRole("checkbox", { name: "My issue moves to Blocked" });
  expect(screen.getAllByRole("checkbox")).toHaveLength(5);
  expect(screen.getByText("Needs my reply")).toBeVisible();
  expect(screen.getByText("Agent updates")).toBeVisible();
  expect(screen.queryByRole("switch", { name: "Assignments" })).toBeNull();
  expect(
    screen.getByRole("switch", { name: "Show system notifications when Multica is in the background" }),
  ).toBeVisible();

  expect(blocked).toHaveAttribute("aria-checked", "true");
  await user.click(blocked);
  await waitFor(() => expect(update).toHaveBeenLastCalledWith({ focused_blocked: "off" }, "acme"));
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: "My issue moves to Blocked" })).toHaveAttribute("aria-checked", "false"),
  );
});

it("renders a saved Focused setup and switches back to All", async () => {
  stored = { inbox_mode: "focused", focused_mentioned: "off" };
  const user = userEvent.setup();
  mount();

  expect(await screen.findByRole("checkbox", { name: "I'm mentioned in a comment" })).toHaveAttribute(
    "aria-checked",
    "false",
  );
  expect(screen.getByRole("checkbox", { name: "An agent replies on my issue" })).toHaveAttribute(
    "aria-checked",
    "true",
  );

  await user.click(screen.getByRole("button", { name: "All" }));
  await waitFor(() => expect(update).toHaveBeenCalledWith({ inbox_mode: "all" }, "acme"));
  expect(await screen.findByRole("switch", { name: "Assignments" })).toBeVisible();
  expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
});

describe("notification setting hierarchy", () => {
  it.each([
    { systemMuted: false, chatMuted: false },
    { systemMuted: false, chatMuted: true },
    { systemMuted: true, chatMuted: false },
    { systemMuted: true, chatMuted: true },
  ])("nests chat replies under system notifications: %j", async ({ systemMuted, chatMuted }) => {
    stored = {
      ...(systemMuted ? { system_notifications: "muted" } : {}),
      ...(chatMuted ? { chat_replies: "muted" } : {}),
    };
    mount();
    // Defaults render until the preferences query resolves; retry until the stored state shows.
    await waitFor(() => {
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
});
