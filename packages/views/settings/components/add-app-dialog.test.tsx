import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { workspaceFeatureKeys, type PaidFeatureAccess } from "@multica/core/billing";
import { toast } from "sonner";
import enBilling from "../../locales/en/billing.json";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppAccountScope, AppCatalogEntry, AppsOverview } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AddAppDialog } from "./add-app-dialog";

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { connectApp: vi.fn(), getAppsOverview: vi.fn() },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Agent" }) }));
vi.mock("../../navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../navigation")>()),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a"> & { href: string }) => <a href={href} {...props}>{children}</a>,
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=accounts"),
    hash: "",
  }),
}));

const overview: AppsOverview = {
  catalog_mode: "all", can_manage: true, agent_apps: [], personal: [],
  team: [{ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null }],
};
const catalog = [
  { slug: "gmail", name: "Gmail", curated: true, recommended_scope: "personal" as const },
  { slug: "notion", name: "Notion", curated: true, recommended_scope: "team" as const },
];

beforeEach(() => {
  vi.mocked(api.connectApp).mockRejectedValue(new Error("offline"));
  vi.mocked(api.getAppsOverview).mockResolvedValue(overview);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderDialog(
  scope: AppAccountScope,
  canManage = true,
  extra: { overview?: AppsOverview; catalog?: AppCatalogEntry[]; teamAccess?: Pick<PaidFeatureAccess, "canAdd" | "state"> } = {},
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  render(<QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings, billing: enBilling } }}>
      <AddAppDialog open onOpenChange={vi.fn()} scope={scope} canManage={canManage}
        overview={extra.overview ?? overview} catalog={extra.catalog ?? catalog} catalogLoading={false} catalogError={false}
        teamAccess={extra.teamAccess} />
    </I18nProvider>
  </QueryClientProvider>);
  return { invalidate };
}

describe("AddAppDialog", () => {
  it("says what agents can do before a team connection starts", async () => {
    const user = userEvent.setup();
    renderDialog("team");
    const dialog = screen.getByRole("dialog", { name: "Connect team account" });
    expect(within(dialog).getAllByRole("button", { name: "Connect" })).toHaveLength(1);
    expect(within(dialog).getByText("Connected")).toBeVisible();
    await user.click(within(dialog).getByRole("button", { name: "Connect" }));
    expect(api.connectApp).not.toHaveBeenCalled();
    const disclosure = await screen.findByRole("dialog", { name: /Notion/ });
    await user.click(within(disclosure).getByRole("button", { name: "Continue" }));
    await waitFor(() =>
      expect(api.connectApp).toHaveBeenCalledWith("team", "notion", "/acme/settings?tab=accounts"),
    );
  });

  it("does not mistake a team connection for the caller's personal connection", async () => {
    const user = userEvent.setup();
    renderDialog("personal", false);
    const dialog = screen.getByRole("dialog", { name: "Connect my account" });
    expect(within(dialog).getAllByRole("button", { name: "Connect" })).toHaveLength(2);
    expect(within(dialog).queryByText("Connected")).not.toBeInTheDocument();
    const row = within(dialog).getByText("Gmail").closest("li")!;
    await user.click(within(row).getByRole("button", { name: "Connect" }));
    const disclosure = await screen.findByRole("dialog", { name: /Gmail/ });
    await user.click(within(disclosure).getByRole("button", { name: "Continue" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("personal", "gmail", expect.any(String)));
  });

  it("guards team connections even if a member is passed a team dialog", async () => {
    const user = userEvent.setup();
    renderDialog("team", false);
    const connect = screen.getByRole("button", { name: "Connect" });
    expect(connect).toBeDisabled();
    await user.click(connect);
    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();
  });

  it("returns to the app list when the disclosure is cancelled", async () => {
    const user = userEvent.setup();
    renderDialog("team");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    const disclosure = await screen.findByRole("dialog", { name: /Notion/ });
    await user.click(within(disclosure).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: /Notion/ })).not.toBeInTheDocument());
    expect(screen.getByRole("dialog", { name: "Connect team account" })).toBeVisible();
    expect(api.connectApp).not.toHaveBeenCalled();
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
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("team", "notion", expect.any(String)));
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
    renderDialog("team", true, {
      overview: withExpiredSlack, catalog: catalogWithSlack, teamAccess: { canAdd: false, state: "read_only" },
    });
    await user.click(screen.getByRole("button", { name: "Reconnect" }));
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    await waitFor(() => expect(api.connectApp).toHaveBeenCalledWith("team", "slack", expect.any(String)));
  });

  it("does not gate personal accounts", () => {
    renderDialog("personal", true, { teamAccess: { canAdd: false, state: "unavailable" } });
    expect(screen.getAllByRole("button", { name: "Connect" })).toHaveLength(2);
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
  });

  it("explains a feature_locked refusal and refetches the plan", async () => {
    const locked = new ApiError("locked", 403, "Forbidden", { code: "feature_locked", feature: "team_app_accounts" });
    vi.mocked(api.connectApp).mockRejectedValue(locked);
    const user = userEvent.setup();
    const { invalidate } = renderDialog("team", true, { teamAccess: { canAdd: true, state: "available" } });
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await user.click(await screen.findByRole("button", { name: "Continue" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enBilling.paid_feature.locked_error));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: workspaceFeatureKeys.all("ws-1") });
  });
});
