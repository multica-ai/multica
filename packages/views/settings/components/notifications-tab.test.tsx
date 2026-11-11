import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import enSettings from "../../locales/en/settings.json";
import { NotificationsTab } from "./notifications-tab";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-a" }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-a", name: "Acme" }) }));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: vi.fn() }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));

afterEach(cleanup);

describe("notification setting hierarchy", () => {
  it.each([false, true])("separates Chat events from system delivery (master muted: %s)", (muted) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
    qc.setQueryData(notificationPreferenceKeys.all("ws-a"), {
      workspace_id: "ws-a", preferences: muted ? { system_notifications: "muted" } : {},
    });
    render(
      <QueryClientProvider client={qc}>
        <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
          <NotificationsTab />
        </I18nProvider>
      </QueryClientProvider>,
    );
    const chat = screen.getByRole("region", { name: "Chat" });
    const system = screen.getByRole("region", { name: "System Notifications" });
    const replies = within(chat).getByRole("switch", { name: "Agent replies" });
    expect(replies).toBeChecked();
    if (muted) {
      expect(replies).toHaveAttribute("aria-disabled", "true");
      expect(within(chat).getByText("Enable system notifications first.")).toBeVisible();
    } else {
      expect(replies).not.toHaveAttribute("aria-disabled");
      expect(within(chat).queryByText("Enable system notifications first.")).toBeNull();
    }
    expect(within(chat).getByText("Unread badges in Chat always stay on.")).toBeVisible();
    expect(within(system).getAllByRole("switch")).toHaveLength(1);
    expect(within(system).getByText("Applies to Inbox notifications and chat replies.")).toBeVisible();
    qc.clear();
  });
});
