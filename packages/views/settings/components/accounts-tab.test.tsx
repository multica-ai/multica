import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AppRequest, AppsOverview } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AccountsTab } from "./accounts-tab";

vi.mock("@multica/core/api", () => ({ api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), listAppRequests: vi.fn(), getPersonalAppImpact: vi.fn(),
  getTeamAppImpact: vi.fn(), deleteComposioConnection: vi.fn(), updateAppRules: vi.fn(), connectApp: vi.fn(),
  requestTeamApp: vi.fn(), dismissTeamAppRequests: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => ({ a1: "Support bot", a2: "Ops bot", alice: "Alice" })[id] ?? id }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "me" } };
  return { useAuthStore: (selector: (s: typeof state) => unknown) => selector(state) };
});
vi.mock("../../common/use-team-app-access", () => ({
  useTeamAppAccess: () => ({ state: "available", canAdd: true, needsUpgrade: () => false }),
}));

let overview: AppsOverview;
const replace = vi.fn();

function request(patch: Partial<AppRequest>): AppRequest {
  return {
    id: "r1", kind: "connect", status: "pending", agent_id: "a1", issue_id: "i1", chat_session_id: null,
    approver_user_id: "me", account_scope: "personal", toolkit_slug: "gmail", tool_slug: "", action_class: "read",
    summary: "", error: null, reason: null, redacted: false, expires_at: "", decided_by: null, decided_at: null,
    created_at: "", ...patch,
  } as AppRequest;
}

beforeEach(() => {
  overview = {
    catalog_mode: "all", can_manage: false,
    team: [
      { id: "t-sentry", toolkit_slug: "sentry", status: "expired", connected_at: "", last_used_at: null },
      { id: "t-posthog", toolkit_slug: "posthog", status: "active", connected_at: "", last_used_at: null },
    ],
    team_requests: [
      { toolkit_slug: "hubspot", requested_by: ["alice"], asked_by: ["alice"], agent_ids: ["a2"], last_requested_at: "" },
    ],
    agent_apps: [
      { agent_id: "a2", toolkit_slug: "posthog", account_scope: "team", level: "read" },
      { agent_id: "a1", toolkit_slug: "notion", account_scope: "personal", level: "read" },
    ],
    personal: [
      { id: "p-notion", toolkit_slug: "notion", status: "active", connected_at: "", last_used_at: null },
      { id: "p-slack", toolkit_slug: "slack", status: "expired", connected_at: "", last_used_at: null },
      { id: "p-old", toolkit_slug: "jira", status: "revoked", connected_at: "", last_used_at: null },
    ],
  };
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "all", apps: [
    { slug: "gmail", name: "Gmail", curated: true },
    { slug: "notion", name: "Notion", curated: true },
    { slug: "slack", name: "Slack", curated: true },
    { slug: "sentry", name: "Sentry", curated: true },
    { slug: "posthog", name: "PostHog", curated: true },
    { slug: "hubspot", name: "HubSpot", curated: true },
  ] });
  vi.mocked(api.listAppRequests).mockResolvedValue({ requests: [
    request({}),
    request({ id: "r2", kind: "action", toolkit_slug: "slack" }),
  ] } as never);
  vi.mocked(api.getPersonalAppImpact).mockResolvedValue({
    agents: [{ id: "a1", name: "Support bot", workspace_id: "ws-1", workspace_name: "Acme", running: true }],
  });
  vi.mocked(api.deleteComposioConnection).mockResolvedValue(undefined as never);
  vi.mocked(api.requestTeamApp).mockResolvedValue(undefined);
  vi.mocked(api.dismissTeamAppRequests).mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderTab(search = "") {
  const adapter: NavigationAdapter = {
    push: vi.fn(), replace, back: vi.fn(), pathname: "/acme/settings",
    searchParams: new URLSearchParams(search), hash: "", getShareableUrl: (p) => p,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <AccountsTab />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

const row = (scope: "team" | "personal", slug: string) =>
  document.querySelector(`[data-account-row="${scope}:${slug}"]`) as HTMLElement;

describe("AccountsTab", () => {
  it("lists team accounts first for a member, with the shared statuses and Ask an admin", async () => {
    renderTab();
    const team = await screen.findByRole("region", { name: "Team accounts" });
    await within(team).findByText("PostHog");
    const order = within(team).getAllByText(/^(HubSpot|Sentry|PostHog)$/).map((el) => el.textContent);
    expect(order).toEqual(["Sentry", "HubSpot", "PostHog"]);
    expect(within(row("team", "sentry")).getAllByText("Needs reconnecting").length).toBeGreaterThan(0);
    expect(within(row("team", "hubspot")).getAllByText(/Requested · admins notified/).length).toBeGreaterThan(0);
    expect(within(row("team", "hubspot")).getByText(/Ops bot needs it/)).toBeInTheDocument();
    // A member cannot reconnect a team account; they ask an admin.
    expect(within(row("team", "sentry")).getByRole("button", { name: "Ask an admin" })).toBeInTheDocument();
    expect(within(row("team", "posthog")).queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Connect team account" })).not.toBeInTheDocument();
  });

  it("lets a member withdraw their own ask", async () => {
    overview.team_requests = [{ toolkit_slug: "hubspot", requested_by: ["me"], asked_by: ["me"], agent_ids: [], last_requested_at: "" }];
    const user = userEvent.setup();
    renderTab();
    await screen.findByText("HubSpot");
    await user.click(within(row("team", "hubspot")).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(api.dismissTeamAppRequests).toHaveBeenCalledWith("hubspot"));
  });

  it("gives an admin Connect and Decline on a requested team account", async () => {
    overview.can_manage = true;
    const user = userEvent.setup();
    renderTab();
    await screen.findByText("HubSpot");
    const hubspot = row("team", "hubspot");
    expect(within(hubspot).getAllByText(/Requested · Ops bot, Alice/).length).toBeGreaterThan(0);
    expect(within(hubspot).getByRole("button", { name: "Connect" })).toBeInTheDocument();
    await user.click(within(hubspot).getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(api.dismissTeamAppRequests).toHaveBeenCalledWith("hubspot"));
    expect(within(row("team", "sentry")).getByRole("button", { name: "Reconnect" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect team account" })).toBeInTheDocument();
  });

  it("lists the viewer's accounts in this workspace, including ones an agent waits for", async () => {
    renderTab();
    const yours = await screen.findByRole("region", { name: "Your accounts" });
    await within(yours).findByText("Notion");
    expect(within(yours).queryByText("jira")).not.toBeInTheDocument();
    expect(within(row("personal", "notion")).getByText(/used by Support bot/)).toBeInTheDocument();
    await waitFor(() => expect(within(row("personal", "gmail")).getByText(/Support bot needs it/)).toBeInTheDocument());
    expect(within(row("personal", "gmail")).getByRole("button", { name: "Connect" })).toBeInTheDocument();
    expect(within(row("personal", "slack")).getByRole("button", { name: "Reconnect" })).toBeInTheDocument();
  });

  it("disconnects after listing the agents that lose the account", async () => {
    const user = userEvent.setup();
    renderTab();
    await screen.findByText("Notion");
    await user.click(within(row("personal", "notion")).getByRole("button", { name: "Notion actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Disconnect" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Disconnect Notion?" });
    await waitFor(() => expect(within(dialog).getByText("Support bot")).toBeInTheDocument());
    expect(within(dialog).getByText("Run in progress — it stops")).toHaveClass("text-warning");
    await user.click(within(dialog).getByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(api.deleteComposioConnection).toHaveBeenCalledWith("p-notion"));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("lets a member lift saved restrictions on their own account after confirming", async () => {
    overview.personal[0] = { ...overview.personal[0]!, actions_blocked: true };
    vi.mocked(api.updateAppRules).mockImplementation(async () => {
      overview = { ...overview, personal: overview.personal.map((c) => ({ ...c, actions_blocked: false })) };
      return { rules: [] };
    });
    const user = userEvent.setup();
    renderTab();
    await user.click(await screen.findByRole("button", { name: "Allow all actions" }));
    expect(api.updateAppRules).not.toHaveBeenCalled();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Allow all actions" }));
    await waitFor(() => expect(api.updateAppRules).toHaveBeenCalledWith("notion", "personal", expect.any(Array)));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Allow all actions" })).not.toBeInTheDocument());
  });

  it("finishes a personal connect return redirected from the retired Apps tab", async () => {
    renderTab("tab=apps&connected=gmail");
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/acme/settings?tab=accounts"));
  });
});
