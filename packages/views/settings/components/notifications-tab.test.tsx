import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { NotificationPreferences } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const prefs = vi.hoisted(() => ({ current: {} as NotificationPreferences }));
const mutate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: { workspace_id: "workspace-1", preferences: prefs.current } }),
  queryOptions: <T,>(opts: T) => opts,
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/notification-preferences/queries", () => ({
  notificationPreferenceOptions: () => ({}),
}));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate }),
}));
vi.mock("./browser-notification-setting", () => ({ BrowserNotificationSetting: () => null }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

import { NotificationsTab } from "./notifications-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const kinds = enSettings.notifications.kinds;
const routes = enSettings.notifications.routes;
const select = (label: string) => screen.getByRole("combobox", { name: label });

beforeEach(() => {
  vi.clearAllMocks();
  prefs.current = {};
});

describe("NotificationsTab", () => {
  it("shows every kind on its default section", () => {
    render(<NotificationsTab />, { wrapper: Wrapper });

    expect(select(kinds.assigned.label)).toHaveTextContent(routes.action.label);
    expect(select(kinds.mentions.label)).toHaveTextContent(routes.action.label);
    expect(select(kinds.status_changes.label)).toHaveTextContent(routes.activity.label);
    expect(select(kinds.agent_comments.label)).toHaveTextContent(routes.activity.label);
    expect(screen.getByRole("button", { name: enSettings.notifications.reset })).toBeDisabled();
  });

  it("shows a muted legacy group as off on both kinds it covered", () => {
    prefs.current = { comments: "muted" };
    render(<NotificationsTab />, { wrapper: Wrapper });

    expect(select(kinds.member_comments.label)).toHaveTextContent(routes.muted.label);
    expect(select(kinds.agent_comments.label)).toHaveTextContent(routes.muted.label);
    expect(select(kinds.mentions.label)).toHaveTextContent(routes.action.label);
  });

  it("routes a kind to the chosen section", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />, { wrapper: Wrapper });

    await user.click(select(kinds.status_changes.label));
    await user.click(await screen.findByRole("option", { name: new RegExp(`^${routes.action.label}`) }));

    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ status_changes: "action" }, expect.anything()),
    );
  });

  it("keeps the other half of a muted legacy group muted when one half changes", async () => {
    const user = userEvent.setup();
    prefs.current = { comments: "muted" };
    render(<NotificationsTab />, { wrapper: Wrapper });

    await user.click(select(kinds.member_comments.label));
    await user.click(await screen.findByRole("option", { name: new RegExp(`^${routes.action.label}`) }));

    await waitFor(() =>
      expect(mutate).toHaveBeenCalledWith({ agent_comments: "muted" }, expect.anything()),
    );
  });

  it("restores every kind to its default and keeps system notifications", async () => {
    const user = userEvent.setup();
    prefs.current = { status_changes: "action", comments: "muted", system_notifications: "muted" };
    render(<NotificationsTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("button", { name: enSettings.notifications.reset }));

    expect(mutate).toHaveBeenCalledWith({ system_notifications: "muted" }, expect.anything());
  });

  it("toggles system notifications without touching routes", async () => {
    const user = userEvent.setup();
    prefs.current = { status_changes: "action" };
    render(<NotificationsTab />, { wrapper: Wrapper });

    await user.click(screen.getByRole("switch", { name: enSettings.notifications.system.label }));

    expect(mutate).toHaveBeenCalledWith(
      { status_changes: "action", system_notifications: "muted" },
      expect.anything(),
    );
  });
});
