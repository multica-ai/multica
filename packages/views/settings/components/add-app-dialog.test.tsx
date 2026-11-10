import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppAccountScope, AppsOverview } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AddAppDialog } from "./add-app-dialog";

vi.mock("@multica/core/api", () => ({ api: { connectApp: vi.fn() } }));

const overview: AppsOverview = {
  catalog_mode: "all", can_manage: true, agent_apps: [], personal: [],
  team: [{ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null }],
};
const catalog = [
  { slug: "gmail", name: "Gmail", curated: true, recommended_scope: "personal" as const },
  { slug: "notion", name: "Notion", curated: true, recommended_scope: "team" as const },
];

beforeEach(() => { vi.mocked(api.connectApp).mockRejectedValue(new Error("offline")); });
afterEach(() => { cleanup(); vi.clearAllMocks(); });

function renderDialog(scope: AppAccountScope, canManage = true) {
  render(<I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
    <AddAppDialog open onOpenChange={vi.fn()} scope={scope} canManage={canManage}
      overview={overview} catalog={catalog} catalogLoading={false} catalogError={false} />
  </I18nProvider>);
}

describe("AddAppDialog", () => {
  it("shows only team connection actions in the team context", async () => {
    const user = userEvent.setup();
    renderDialog("team");
    const dialog = screen.getByRole("dialog", { name: "Connect team account" });
    expect(within(dialog).getAllByRole("button", { name: "Connect" })).toHaveLength(1);
    expect(within(dialog).getByText("Connected")).toBeVisible();
    await user.click(within(dialog).getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("team", "notion"));
  });

  it("does not mistake a team connection for the caller's personal connection", async () => {
    const user = userEvent.setup();
    renderDialog("personal", false);
    const dialog = screen.getByRole("dialog", { name: "Connect my account" });
    expect(within(dialog).getAllByRole("button", { name: "Connect" })).toHaveLength(2);
    expect(within(dialog).queryByText("Connected")).not.toBeInTheDocument();
    const row = within(dialog).getByText("Gmail").closest("li")!;
    await user.click(within(row).getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("personal", "gmail"));
  });

  it("guards team connections even if a member is passed a team dialog", async () => {
    const user = userEvent.setup();
    renderDialog("team", false);
    const connect = screen.getByRole("button", { name: "Connect" });
    expect(connect).toBeDisabled();
    await user.click(connect);
    expect(api.connectApp).not.toHaveBeenCalled();
  });

  it("keeps the scoped dialog open after failure and permits retry", async () => {
    const user = userEvent.setup();
    renderDialog("team");
    const connect = screen.getByRole("button", { name: "Connect" });
    await user.click(connect);
    await waitFor(() => expect(connect).toBeEnabled());
    expect(screen.getByRole("dialog", { name: "Connect team account" })).toBeVisible();
    await user.click(connect);
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledTimes(2));
    expect(api.connectApp).toHaveBeenLastCalledWith("team", "notion");
  });
});
