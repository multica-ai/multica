import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { api, ApiError } from "@multica/core/api";
import type { PaidFeatureAccess } from "@multica/core/billing";
import { toast } from "sonner";
import enBilling from "../../locales/en/billing.json";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppAccountScope, AppsOverview } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AddAppDialog } from "./add-app-dialog";

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a"> & { href: string }) => <a href={href} {...props}>{children}</a>,
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { connectApp: vi.fn() },
}));

const overview: AppsOverview = {
  catalog_mode: "all", can_manage: true, agent_apps: [], personal: [],
  team: [{ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null }],
};
const catalog = [
  { slug: "gmail", name: "Gmail", curated: true, recommended_scope: "personal" as const },
  { slug: "notion", name: "Notion", curated: true, recommended_scope: "team" as const },
];

beforeEach(() => { vi.mocked(api.connectApp).mockRejectedValue(new Error("offline")); });
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  delete (window as { desktopAPI?: unknown }).desktopAPI;
});

function renderDialog(
  scope: AppAccountScope,
  canManage = true,
  extra: { overview?: AppsOverview; teamAccess?: Pick<PaidFeatureAccess, "canAdd" | "state">; onConnectError?: (e: unknown) => void } = {},
) {
  render(<I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings, billing: enBilling } }}>
    <AddAppDialog open onOpenChange={vi.fn()} scope={scope} canManage={canManage}
      overview={extra.overview ?? overview} catalog={catalog} catalogLoading={false} catalogError={false}
      teamAccess={extra.teamAccess} onConnectError={extra.onConnectError} />
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

  it("hands the hosted flow to the system browser on Desktop and frees the row", async () => {
    // Desktop blocks same-window navigation to another origin, so assigning
    // the location left the row on "Connecting..." forever.
    const openExternal = vi.fn();
    (window as { desktopAPI?: unknown }).desktopAPI = { openExternal, pickDirectory: vi.fn() };
    vi.mocked(api.connectApp).mockResolvedValue({ redirect_url: "https://connect.example.test/flow" });
    const user = userEvent.setup();
    renderDialog("team");
    const connect = screen.getByRole("button", { name: "Connect" });
    await user.click(connect);
    await waitFor(() => expect(openExternal).toHaveBeenCalledWith("https://connect.example.test/flow"));
    await waitFor(() => expect(connect).toBeEnabled());
    expect(connect).toHaveTextContent("Connect");
  });
});

describe("AddAppDialog on a plan without team app accounts (MUL-9491)", () => {
  const withExpiredSlack: AppsOverview = {
    ...overview,
    team: [...overview.team, { id: "team-slack", toolkit_slug: "slack", status: "expired", connected_at: "", last_used_at: null }],
  };
  const catalogWithSlack = [...catalog, { slug: "slack", name: "Slack", curated: true }];

  it("connects a new team app normally when the plan includes it", async () => {
    const user = userEvent.setup();
    renderDialog("team", true, { teamAccess: { canAdd: true, state: "available" } });
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("team", "notion"));
  });

  it("replaces Connect with Upgrade to add for an app without a team account", async () => {
    const user = userEvent.setup();
    renderDialog("team", true, { teamAccess: { canAdd: false, state: "read_only" } });
    expect(screen.getByText("Read-only. Upgrade to edit.")).toBeVisible();
    const upgrade = screen.getByRole("link", { name: "Upgrade to add" });
    expect(upgrade).toHaveAttribute("href", "/acme/settings?tab=billing");
    expect(screen.queryByRole("button", { name: "Connect" })).not.toBeInTheDocument();
    await user.click(upgrade);
    expect(api.connectApp).not.toHaveBeenCalled();
  });

  it("still reconnects an app's existing team account", async () => {
    const user = userEvent.setup();
    render(<I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings, billing: enBilling } }}>
      <AddAppDialog open onOpenChange={vi.fn()} scope="team" canManage overview={withExpiredSlack}
        catalog={catalogWithSlack} catalogLoading={false} catalogError={false}
        teamAccess={{ canAdd: false, state: "read_only" }} />
    </I18nProvider>);
    await user.click(screen.getByRole("button", { name: "Reconnect" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("team", "slack"));
  });

  it("does not gate personal accounts", () => {
    renderDialog("personal", true, { teamAccess: { canAdd: false, state: "unavailable" } });
    expect(screen.getAllByRole("button", { name: "Connect" })).toHaveLength(2);
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
  });

  it("explains a feature_locked refusal and reports it to the caller", async () => {
    const locked = new ApiError("locked", 403, "Forbidden", { code: "feature_locked", feature: "team_app_accounts" });
    vi.mocked(api.connectApp).mockRejectedValue(locked);
    const onConnectError = vi.fn();
    const user = userEvent.setup();
    renderDialog("team", true, { teamAccess: { canAdd: true, state: "available" }, onConnectError });
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enBilling.paid_feature.locked_error));
    expect(onConnectError).toHaveBeenCalledWith(locked);
  });
});
