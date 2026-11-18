import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AppAccountScope, AppsOverview } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AddAppDialog } from "./add-app-dialog";

vi.mock("@multica/core/api", () => ({ api: { connectApp: vi.fn(), getAppsOverview: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Agent" }) }));
vi.mock("../../navigation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../navigation")>()),
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams("tab=connected-accounts"),
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

function renderDialog(scope: AppAccountScope, canManage = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <AddAppDialog open onOpenChange={vi.fn()} scope={scope} canManage={canManage}
        overview={overview} catalog={catalog} catalogLoading={false} catalogError={false} />
    </I18nProvider>
  </QueryClientProvider>);
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
      expect(api.connectApp).toHaveBeenCalledWith("team", "notion", "/acme/settings?tab=connected-accounts"),
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
