import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import enSettings from "../../locales/en/settings.json";
import { NotificationsTab } from "./notifications-tab";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-a" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-a", name: "Acme" }),
  useWorkspacePaths: () => ({ inbox: () => "/acme/inbox" }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: ReactNode; className?: string }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: vi.fn() }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));

afterEach(cleanup);

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
  return qc;
}

describe("Inbox notification settings", () => {
  it("groups the events by how they reach you", () => {
    renderTab({ updates: "muted" });
    const inbox = screen.getByRole("region", { name: "Inbox" });
    const lines = Array.from(
      inbox.querySelectorAll("[data-settings-anchor] .font-medium, .text-caption.font-medium"),
      (el) => el.textContent,
    );
    expect(lines).toEqual([
      "About you",
      "Assignments",
      "Mentions",
      "Reactions",
      "Issues you subscribe to",
      "Status changes",
      "Comments",
      "Priority & Due date",
      "Projects you subscribe to",
      "New issues",
      "Agents",
      "Agent activity",
    ]);
    expect(within(inbox).getByRole("switch", { name: "Priority & Due date" })).not.toBeChecked();
    expect(within(inbox).getByRole("switch", { name: "Comments" })).toBeChecked();
  });

  it("explains how notifications leave the Inbox, with a way to the archived ones", () => {
    renderTab();
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    expect(leaving).toHaveTextContent("Reading a notification doesn't take it out of the Inbox.");
    expect(leaving).toHaveTextContent("Issues others complete stay in your Inbox.");
    expect(within(leaving).getByRole("link", { name: "View archived" })).toHaveAttribute(
      "href",
      "/acme/inbox?view=archived",
    );
  });

  it.each<{ preferences: Record<string, string>; checked: boolean }>([
    { preferences: {}, checked: true },
    { preferences: { auto_archive_completed: "muted" }, checked: false },
  ])("toggles archiving issues you complete: %j", ({ preferences, checked }) => {
    renderTab(preferences);
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    const toggle = within(leaving).getByRole("switch", { name: "Archive issues you complete" });
    if (checked) expect(toggle).toBeChecked();
    else expect(toggle).not.toBeChecked();
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
