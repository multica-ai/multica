import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AppRequest, AppsOverview } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { ConnectedAccountsTab } from "./connected-accounts-tab";

vi.mock("@multica/core/api", () => ({ api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), listAppRequests: vi.fn(), getPersonalAppImpact: vi.fn(),
  deleteComposioConnection: vi.fn(), updateAppRules: vi.fn(), getIssue: vi.fn(), connectApp: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/acme/issues/${id}`,
    chatSession: (id: string) => `/acme/chat?session=${id}`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "a1" ? "Support bot" : id) }),
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
    catalog_mode: "all", can_manage: false, team: [], agent_apps: [],
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
  ] });
  vi.mocked(api.listAppRequests).mockResolvedValue({ requests: [
    request({}),
    request({ id: "r2", kind: "action", toolkit_slug: "slack" }),
    request({ id: "r3", account_scope: "team", toolkit_slug: "slack" }),
  ] } as never);
  vi.mocked(api.getIssue).mockResolvedValue({ id: "i1", identifier: "MUL-12" } as never);
  vi.mocked(api.getPersonalAppImpact).mockImplementation(async (id: string) => ({
    agents: id === "p-notion"
      ? [
          { id: "a1", name: "Support bot", workspace_id: "ws-1", workspace_name: "Acme", running: true },
          { id: "a9", name: "Docs bot", workspace_id: "ws-2", workspace_name: "Side project", running: false },
        ]
      : [],
  }));
  vi.mocked(api.deleteComposioConnection).mockResolvedValue(undefined as never);
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
        <ConnectedAccountsTab />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

describe("ConnectedAccountsTab", () => {
  it("lists the viewer's live accounts with who uses them", async () => {
    renderTab();
    expect(await screen.findByText("Notion")).toBeInTheDocument();
    expect(screen.queryByText("jira")).not.toBeInTheDocument();
    expect(await screen.findByText(/Used by Support bot, Docs bot/)).toBeInTheDocument();
    expect(screen.getByText("Sign-in expired")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Reconnect" })).toHaveLength(1);
  });

  it("shows only personal connect requests waiting on the viewer", async () => {
    renderTab();
    const section = await screen.findByRole("region", { name: "Waiting for you" });
    await waitFor(() => expect(within(section).getByText("Support bot needs your Gmail on MUL-12")).toBeInTheDocument());
    expect(within(section).getByRole("button", { name: "Open issue" })).toHaveAttribute("href", "/acme/issues/i1");
    expect(within(section).getAllByRole("button", { name: "Connect Gmail" })).toHaveLength(1);
    expect(within(section).queryByText(/Slack/)).not.toBeInTheDocument();
  });

  it("hides the waiting section when nothing waits", async () => {
    vi.mocked(api.listAppRequests).mockResolvedValue({ requests: [] } as never);
    renderTab();
    await screen.findByText("Notion");
    expect(screen.queryByRole("region", { name: "Waiting for you" })).not.toBeInTheDocument();
  });

  it("disconnects everywhere after listing the agents that lose the account", async () => {
    const user = userEvent.setup();
    renderTab();
    await user.click(await screen.findByRole("button", { name: "Notion actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Disconnect" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Disconnect Notion?" });
    await waitFor(() => expect(within(dialog).getByText("Side project")).toBeInTheDocument());
    expect(within(dialog).getByText("has a run in progress — it stops")).toHaveClass("text-warning");
    await user.click(within(dialog).getByRole("button", { name: "Disconnect everywhere" }));
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
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/acme/settings?tab=connected-accounts"));
  });
});
