import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppsOverview } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AppsSettings } from "./apps-settings";

vi.mock("@multica/core/api", () => ({ api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), updateAppSettings: vi.fn(), updateAppRules: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Admin" }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ searchParams: new URLSearchParams() }) }));

let overview: AppsOverview;
beforeEach(() => {
  overview = {
    catalog_mode: "custom", allowed_toolkits: [], can_manage: true, agent_apps: [],
    team: [{ id: "team-1", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null, actions_blocked: true }],
    personal: [{ id: "personal-1", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null, actions_blocked: true }],
  };
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "custom", apps: [] });
  vi.mocked(api.updateAppSettings).mockImplementation(async () => {
    overview = { ...overview, catalog_mode: "all" };
    return { catalog_mode: "all", allowed_toolkits: [] };
  });
  vi.mocked(api.updateAppRules).mockImplementation(async (_slug, scope) => {
    overview = { ...overview, [scope]: overview[scope].map((c) => ({ ...c, actions_blocked: false })) };
    return { rules: [] };
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  delete (window as { desktopAPI?: unknown }).desktopAPI;
});

function renderSettings() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <AppsSettings />
    </I18nProvider>
  </QueryClientProvider>);
}

it("recovers an empty saved catalog only after the admin confirms", async () => {
  const user = userEvent.setup();
  renderSettings();
  await user.click(await screen.findByRole("button", { name: "Show all apps" }));
  expect(api.updateAppSettings).not.toHaveBeenCalled();
  await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Show all apps" }));
  await waitFor(() => expect(api.updateAppSettings).toHaveBeenCalledWith({ catalog_mode: "all", allowed_toolkits: [] }));
  await waitFor(() => expect(screen.queryByText("A saved app list is hiding other apps.")).not.toBeInTheDocument());
});

it("lets a member recover only their personal account and keeps failed confirmation open", async () => {
  overview.can_manage = false;
  vi.mocked(api.updateAppRules).mockRejectedValueOnce(new Error("offline"));
  const user = userEvent.setup();
  renderSettings();
  await screen.findByText("A saved app list is hiding other apps.");
  expect(screen.queryByRole("button", { name: "Show all apps" })).not.toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "Allow all actions" })).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Allow all actions" }));
  expect(api.updateAppRules).not.toHaveBeenCalled();
  const confirm = within(screen.getByRole("alertdialog")).getByRole("button", { name: "Allow all actions" });
  await user.click(confirm);
  await waitFor(() => expect(confirm).toBeEnabled());
  expect(screen.getByRole("alertdialog")).toBeVisible();
  await user.click(confirm);
  await waitFor(() => expect(api.updateAppRules).toHaveBeenLastCalledWith("gmail", "personal", [
    { action_class: "read", policy: "allow" }, { action_class: "write", policy: "allow" },
    { action_class: "send", policy: "allow" }, { action_class: "delete", policy: "allow" },
    { action_class: "share", policy: "allow" },
  ]));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  expect(screen.getAllByText("Saved action restrictions are still blocking this account.")).toHaveLength(1);
});

it("opens a connection dialog for the section that launched it", async () => {
  const user = userEvent.setup();
  renderSettings();
  expect(await screen.findByRole("button", { name: "Connect team account" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Add app" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Connect team account" }));
  expect(screen.getByRole("dialog", { name: "Connect team account" })).toBeVisible();
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  await user.click(screen.getByRole("button", { name: "Connect my account" }));
  expect(screen.getByRole("dialog", { name: "Connect my account" })).toBeVisible();
});

it("only offers members the personal connection entry", async () => {
  overview.can_manage = false;
  renderSettings();
  expect(await screen.findByRole("button", { name: "Connect my account" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Connect team account" })).not.toBeInTheDocument();
});

it("reloads accounts when the Desktop window is focused again after connecting in the browser", async () => {
  (window as { desktopAPI?: unknown }).desktopAPI = { pickDirectory: vi.fn() };
  renderSettings();
  await screen.findByText("A saved app list is hiding other apps.");
  expect(api.getAppsOverview).toHaveBeenCalledTimes(1);
  fireEvent.focus(window);
  await waitFor(() => expect(api.getAppsOverview).toHaveBeenCalledTimes(2));
});

it("does not reload on focus on the web, where the flow returns to this tab", async () => {
  renderSettings();
  await screen.findByText("A saved app list is hiding other apps.");
  fireEvent.focus(window);
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(api.getAppsOverview).toHaveBeenCalledTimes(1);
});
