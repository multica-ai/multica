import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import enSettings from "../../locales/en/settings.json";
import { NotificationsTab } from "./notifications-tab";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-a" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-a", name: "Acme" }) }));
const mutate = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));

afterEach(() => {
  cleanup();
  mutate.mockClear();
});

function renderTab(preferences: Record<string, string> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  qc.setQueryData(notificationPreferenceKeys.all("ws-a"), { workspace_id: "ws-a", preferences });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
        <NotificationsTab />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("inbox notification settings", () => {
  it("offers no way to turn off what is addressed to the user", () => {
    renderTab();
    const needsYou = screen.getByRole("region", { name: "Needs you" });

    for (const label of ["Mentions", "Assignments", "Runs you started", "Issues you created or are assigned"]) {
      expect(within(needsYou).getByText(label)).toBeInTheDocument();
    }
    expect(within(needsYou).getAllByText("Always on")).toHaveLength(4);
    // The one choice: whether a mention of a group counts as the user's.
    expect(within(needsYou).getAllByRole("switch")).toHaveLength(1);
    expect(within(needsYou).getByRole("switch", { name: "@all and @squad" })).toBeChecked();
  });

  it("sends group mentions to Updates when their switch is off", () => {
    renderTab({ comments: "muted" });

    fireEvent.click(screen.getByRole("switch", { name: "@all and @squad" }));

    expect(mutate).toHaveBeenCalledWith({ comments: "muted", mentions: "muted" }, expect.anything());
  });

  it("lets each kind of update be turned off", () => {
    renderTab({ status_changes: "muted" });
    const updates = screen.getByRole("region", { name: "Updates" });

    expect(within(updates).getAllByRole("switch").map((el) => el.getAttribute("aria-label"))).toEqual([
      "Comments",
      "Status changes",
      "Assignment changes",
      "Priority & Due date",
      "New issues",
      "Agent runs",
    ]);
    expect(within(updates).getByRole("switch", { name: "Status changes" })).not.toBeChecked();

    fireEvent.click(within(updates).getByRole("switch", { name: "Status changes" }));
    expect(mutate).toHaveBeenCalledWith({}, expect.anything());
  });
});

describe("notification setting hierarchy", () => {
  it.each([
    { systemMuted: false, chatMuted: false },
    { systemMuted: false, chatMuted: true },
    { systemMuted: true, chatMuted: false },
    { systemMuted: true, chatMuted: true },
  ])("nests chat replies under system notifications: %j", ({ systemMuted, chatMuted }) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    qc.setQueryData(notificationPreferenceKeys.all("ws-a"), {
      workspace_id: "ws-a",
      preferences: {
        ...(systemMuted ? { system_notifications: "muted" } : {}),
        ...(chatMuted ? { chat_replies: "muted" } : {}),
      },
    });
    render(
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
          <NotificationsTab />
        </I18nProvider>
      </QueryClientProvider>,
    );
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
    qc.clear();
  });
});
