import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
const { mutate, resolve } = vi.hoisted(() => ({ mutate: vi.fn(), resolve: vi.fn() }));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate }),
  useResolveAutoArchivePrompt: () => ({ mutate: resolve }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));

afterEach(() => {
  cleanup();
  mutate.mockReset();
  resolve.mockReset();
});

function renderTab(preferences: Record<string, string> = {}, { prompt = false } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  qc.setQueryData(notificationPreferenceKeys.all("ws-a"), {
    workspace_id: "ws-a",
    preferences,
    auto_archive_prompt: prompt,
  });
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
      "Other people's issues",
      "Status changes",
      "Comments",
      "Priority & due date",
      "Projects you subscribe to",
      "New issues",
      "Agents",
      "Agent activity",
    ]);
    expect(within(inbox).getByRole("switch", { name: "Priority & due date" })).not.toBeChecked();
    expect(within(inbox).getByRole("switch", { name: "Comments" })).toBeChecked();
  });

  it("leaves status changes off until you turn them on", async () => {
    const user = userEvent.setup();
    renderTab();
    const statusChanges = within(screen.getByRole("region", { name: "Inbox" })).getByRole("switch", {
      name: "Status changes",
    });
    expect(statusChanges).not.toBeChecked();

    await user.click(statusChanges);
    expect(mutate).toHaveBeenCalledWith({ status_changes: "all" }, expect.anything());
  });

  it("shows status changes on once you turned them on", () => {
    renderTab({ status_changes: "all" });
    const inbox = screen.getByRole("region", { name: "Inbox" });
    expect(within(inbox).getByRole("switch", { name: "Status changes" })).toBeChecked();
  });

  it.each<{ preferences: Record<string, string>; shown: string }>([
    { preferences: {}, shown: "Key updates only" },
    { preferences: { others_issues: "key_updates" }, shown: "Key updates only" },
    { preferences: { others_issues: "all" }, shown: "All activity" },
  ])("shows how much of other people's issues reaches you: %j", ({ preferences, shown }) => {
    renderTab(preferences);
    const inbox = screen.getByRole("region", { name: "Inbox" });
    expect(within(inbox).getByRole("combobox", { name: "Other people's issues" })).toHaveTextContent(shown);
  });

  it("explains how notifications leave the Inbox, with a way to the archived ones", () => {
    renderTab();
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    expect(leaving).toHaveTextContent("Reading a notification doesn't take it out of the Inbox.");
    expect(leaving).toHaveTextContent("Only your own actions count.");
    expect(within(leaving).getByRole("link", { name: "View archived" })).toHaveAttribute(
      "href",
      "/acme/inbox?view=archived",
    );
  });

  const ALL_ACTIONS_OFF = {
    auto_archive_completed: "muted",
    auto_archive_on_agent_trigger: "muted",
    auto_archive_on_comment: "muted",
    auto_archive_on_status: "muted",
    auto_archive_on_assignee: "muted",
  };
  const ACTIONS_OFF_BUT = (on: string[]) =>
    Object.fromEntries(
      [
        "auto_archive_completed",
        "auto_archive_on_agent_trigger",
        "auto_archive_on_comment",
        "auto_archive_on_status",
        "auto_archive_on_assignee",
        "auto_archive_on_priority",
        "auto_archive_on_project",
        "auto_archive_on_dates",
        "auto_archive_on_labels",
      ].map((key) => [key, on.includes(key) ? "all" : "muted"]),
    );
  const archiveChoice = () =>
    within(screen.getByRole("region", { name: "Leaving the Inbox" })).getByRole("combobox", {
      name: "Archive issues you handle",
    });

  it.each<{ preferences: Record<string, string>; shown: string; custom: boolean }>([
    { preferences: {}, shown: "When you complete it or trigger an agent", custom: false },
    { preferences: { ...ALL_ACTIONS_OFF, auto_archive_completed: "all" }, shown: "When you complete it", custom: false },
    { preferences: ALL_ACTIONS_OFF, shown: "Never", custom: false },
    { preferences: { auto_archive_on_labels: "all" }, shown: "Custom", custom: true },
  ])("names the choice your actions match: %j", ({ preferences, shown, custom }) => {
    renderTab(preferences);
    expect(archiveChoice()).toHaveTextContent(shown);
    expect(screen.queryByRole("checkbox", { name: "Change labels" }) !== null).toBe(custom);
  });

  it("offers the presets, the recommended one marked, then a custom choice", async () => {
    const user = userEvent.setup();
    renderTab();
    await user.click(archiveChoice());
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => [option.textContent, option.getAttribute("aria-selected")])).toEqual([
      ["Never", "false"],
      ["When you complete it", "false"],
      ["When you complete it or trigger an agentRecommended", "true"],
      ["Custom...", "false"],
    ]);
  });

  it("saves every action when you pick a preset", async () => {
    const user = userEvent.setup();
    renderTab();
    await user.click(archiveChoice());
    await user.click(await screen.findByRole("option", { name: "When you complete it" }));
    expect(mutate).toHaveBeenCalledWith(ACTIONS_OFF_BUT(["auto_archive_completed"]), expect.anything());
  });

  it("lists every action under a custom choice, grouped, with yours checked", async () => {
    const user = userEvent.setup();
    renderTab();
    await user.click(archiveChoice());
    await user.click(await screen.findByRole("option", { name: "Custom..." }));
    expect(mutate).not.toHaveBeenCalled();
    expect(archiveChoice()).toHaveTextContent("Custom");
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    expect(leaving).toHaveTextContent("Handling it");
    expect(leaving).toHaveTextContent("Tidying it");
    const checked = within(leaving)
      .getAllByRole("checkbox")
      .map((box) => [box.closest("label")?.textContent, box.getAttribute("aria-checked")]);
    expect(checked).toEqual([
      ["Complete", "true"],
      ["Trigger an agent", "true"],
      ["Reply", "false"],
      ["Move it forward", "false"],
      ["Reassign", "false"],
      ["Change priority", "false"],
      ["Change project", "false"],
      ["Change dates", "false"],
      ["Change labels", "false"],
    ]);

    await user.click(within(leaving).getByRole("checkbox", { name: "Change labels" }));
    expect(mutate).toHaveBeenCalledWith(
      ACTIONS_OFF_BUT([
        "auto_archive_completed",
        "auto_archive_on_agent_trigger",
        "auto_archive_on_labels",
      ]),
      expect.anything(),
    );
  });

  it("answers the pending one-time question when you choose here", async () => {
    const user = userEvent.setup();
    renderTab({ ...ALL_ACTIONS_OFF, auto_archive_completed: "all" }, { prompt: true });
    await user.click(archiveChoice());
    await user.click(await screen.findByRole("option", { name: "Never" }));
    expect(resolve).toHaveBeenCalledWith(ACTIONS_OFF_BUT([]), expect.anything());
    expect(mutate).not.toHaveBeenCalled();
  });

  it("nests where to go after archiving under the archive choice, hidden while it is off", () => {
    renderTab(ALL_ACTIONS_OFF);
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    expect(within(leaving).queryByRole("combobox", { name: "After archiving" })).toBeNull();
  });

  it.each<{ preferences: Record<string, string>; shown: string }>([
    { preferences: {}, shown: "Stay on the issue" },
    { preferences: { after_auto_archive: "next" }, shown: "Open the next one" },
  ])("shows where to go after archiving: %j", ({ preferences, shown }) => {
    renderTab(preferences);
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    expect(within(leaving).getByRole("combobox", { name: "After archiving" })).toHaveTextContent(shown);
  });

  it("saves opening the next notification after archiving", async () => {
    const user = userEvent.setup();
    renderTab();
    const leaving = screen.getByRole("region", { name: "Leaving the Inbox" });
    await user.click(within(leaving).getByRole("combobox", { name: "After archiving" }));
    await user.click(await screen.findByRole("option", { name: "Open the next one" }));
    expect(mutate).toHaveBeenCalledWith(
      { after_auto_archive: "next" },
      expect.anything(),
    );
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
    const system = screen.getByRole("region", { name: "System notifications" });
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
