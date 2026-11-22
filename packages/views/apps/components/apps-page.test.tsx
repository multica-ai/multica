import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useEffect, useState } from "react";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AppRequest, AppsOverview } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AppsPage } from "./apps-page";

vi.mock("@multica/core/api", () => ({ api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), listAgents: vi.fn(), listMembers: vi.fn(),
  updateAppSettings: vi.fn(), requestTeamApp: vi.fn(), connectApp: vi.fn(), listAppRequests: vi.fn(),
  getPersonalAppImpact: vi.fn(), getTeamAppImpact: vi.fn(), deleteComposioConnection: vi.fn(), dismissTeamAppRequests: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({ apps: () => "/acme/apps", appDetail: (slug: string) => `/acme/apps/${slug}` }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => `Name ${id}`,
    getActorInitials: () => "N",
    getActorAvatarUrl: () => null,
  }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "me" } };
  return { useAuthStore: (selector: (s: typeof state) => unknown) => selector(state) };
});
const composio = vi.hoisted(() => ({ available: true }));
vi.mock("../../common/use-composio-available", () => ({ useComposioAvailable: () => composio.available }));

let overview: AppsOverview;
const nav = { search: "" };
const replace = vi.fn();

beforeEach(() => {
  composio.available = true;
  nav.search = "";
  overview = {
    catalog_mode: "all", can_manage: false,
    team: [{ id: "t-notion", toolkit_slug: "notion", status: "active", connected_at: "", last_used_at: null }],
    personal: [],
    agent_apps: [
      { agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "write" },
      { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "write" },
      { agent_id: "a2", toolkit_slug: "slack", account_scope: "team", level: "write" },
    ],
  };
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "all", apps: [
    { slug: "gmail", name: "Gmail", curated: true },
    { slug: "notion", name: "Notion", curated: true },
    { slug: "slack", name: "Slack", curated: true },
    { slug: "linear", name: "Linear", category: "Issues", curated: true },
  ] });
  vi.mocked(api.listAgents).mockResolvedValue([
    { id: "a1", name: "Support", owner_id: "me", permission_mode: "private", invocation_targets: [], archived_at: null },
    { id: "a2", name: "Triage", owner_id: "u2", permission_mode: "private", invocation_targets: [], archived_at: null },
  ] as never);
  vi.mocked(api.listMembers).mockResolvedValue([{ user_id: "me", role: "member" }] as never);
  vi.mocked(api.requestTeamApp).mockResolvedValue(undefined as never);
  vi.mocked(api.listAppRequests).mockResolvedValue({ requests: [] } as never);
  vi.mocked(api.dismissTeamAppRequests).mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

// Re-renders with the new search on replace, like the web and desktop routers.
function RoutedAppsPage({ search }: { search: string }) {
  const [current, setCurrent] = useState(search);
  useEffect(() => setCurrent(search), [search]);
  const adapter: NavigationAdapter = {
    push: vi.fn(), back: vi.fn(), pathname: "/acme/apps",
    replace: (url) => {
      replace(url);
      setCurrent(url.split("?")[1] ?? "");
    },
    searchParams: new URLSearchParams(current), hash: "", getShareableUrl: (p) => p,
  };
  return (
    <NavigationProvider value={adapter}>
      <AppsPage />
    </NavigationProvider>
  );
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (search: string) => (
    <QueryClientProvider client={client}>
      <RoutedAppsPage search={search} />
    </QueryClientProvider>
  );
  const view = renderWithI18n(tree(nav.search));
  return { ...view, navigate: (search: string) => view.rerender(tree(search)) };
}

describe("AppsPage", () => {
  it("groups apps in use, puts the ones needing attention first and counts them", async () => {
    renderPage();
    await waitFor(() =>
      expect(within(screen.getAllByRole("list")[0]!).getAllByRole("link").map((l) => l.textContent))
        .toEqual(["Gmail", "Slack", "Notion"]),
    );
    const list = screen.getAllByRole("list")[0];
    expect(screen.getByRole("tab", { name: /Needs attention/ })).toHaveTextContent("2");
    expect(within(list!).getByRole("link", { name: "Notion" })).toHaveAttribute("href", "/acme/apps/notion");
    // A card with nothing to do names its agents in place of an action.
    expect(within(list!).getAllByRole("img", { name: /^Used by / }).map((el) => el.getAttribute("aria-label")))
      .toEqual(["Used by Name a1"]);
    expect(screen.getByRole("heading", { name: "Popular in Acme" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Linear" })).toBeInTheDocument();
  });

  it("asks an admin for a team account a member cannot connect", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Ask an admin" }));
    await waitFor(() => expect(api.requestTeamApp).toHaveBeenCalledWith("slack", undefined));
  });

  it("opens the connect dialog instead of starting the flow", async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Connect" }));
    expect(api.connectApp).not.toHaveBeenCalled();
    expect(await screen.findByRole("dialog", { name: /Connect Gmail/ })).toBeVisible();
  });

  it("filters by search across every tab", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("tab", { name: /In use/ });
    await user.click(screen.getByRole("tab", { name: /All apps/ }));
    await user.type(screen.getByRole("searchbox", { name: "Search apps" }), "iss");
    expect(within(screen.getAllByRole("list")[0]!).getAllByRole("link").map((l) => l.textContent)).toEqual(["Linear"]);
  });

  it("recovers an empty saved catalog only after an admin confirms", async () => {
    overview = { ...overview, catalog_mode: "custom", allowed_toolkits: [], can_manage: true };
    vi.mocked(api.updateAppSettings).mockImplementation(async () => {
      overview = { ...overview, catalog_mode: "all" };
      return { catalog_mode: "all", allowed_toolkits: [] };
    });
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Show all apps" }));
    expect(api.updateAppSettings).not.toHaveBeenCalled();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Show all apps" }));
    await waitFor(() => expect(api.updateAppSettings).toHaveBeenCalledWith({ catalog_mode: "all", allowed_toolkits: [] }));
    await waitFor(() => expect(screen.queryByText("A previous app list is hiding other apps.")).not.toBeInTheDocument());
  });

  it("shows members the saved catalog without a way to lift it", async () => {
    overview = { ...overview, catalog_mode: "custom", allowed_toolkits: [] };
    renderPage();
    expect(await screen.findByText("A previous app list is hiding other apps.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show all apps" })).not.toBeInTheDocument();
  });

  it("reports a hosted connect return once and drops its params", async () => {
    nav.search = "app_connected=gmail&keep=1";
    renderPage();
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/acme/apps?keep=1"));
  });

  it("reloads accounts when the Desktop window is focused again", async () => {
    (window as { desktopAPI?: unknown }).desktopAPI = { pickDirectory: vi.fn() };
    try {
      renderPage();
      await screen.findByRole("tab", { name: /In use/ });
      expect(api.getAppsOverview).toHaveBeenCalledTimes(1);
      fireEvent.focus(window);
      await waitFor(() => expect(api.getAppsOverview).toHaveBeenCalledTimes(2));
    } finally {
      delete (window as { desktopAPI?: unknown }).desktopAPI;
    }
  });

  it("does not reload on focus on the web, where the flow returns to this tab", async () => {
    renderPage();
    await screen.findByRole("tab", { name: /In use/ });
    fireEvent.focus(window);
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(api.getAppsOverview).toHaveBeenCalledTimes(1);
  });

  it("says Apps are unavailable when the server has no Composio", () => {
    composio.available = false;
    renderPage();
    expect(screen.getByText("Apps aren't available on this server.")).toBeInTheDocument();
    expect(api.getAppsOverview).not.toHaveBeenCalled();
  });
});

function connectRequest(patch: Partial<AppRequest>): AppRequest {
  return {
    id: "r1", kind: "connect", status: "pending", agent_id: "a1", issue_id: "i1", chat_session_id: null,
    approver_user_id: "me", account_scope: "personal", toolkit_slug: "linear", tool_slug: "", action_class: "read",
    summary: "", error: null, reason: null, redacted: false, expires_at: "", decided_by: null, decided_at: null,
    created_at: "", ...patch,
  } as AppRequest;
}

describe("AppsPage › Accounts", () => {
  const row = (scope: "team" | "personal", slug: string) =>
    document.querySelector(`[data-account-row="${scope}:${slug}"]`) as HTMLElement;

  beforeEach(() => {
    nav.search = "tab=accounts";
    overview = {
      ...overview,
      team: [
        { id: "t-notion", toolkit_slug: "notion", status: "expired", connected_at: "", last_used_at: null },
        { id: "t-slack", toolkit_slug: "slack", status: "active", connected_at: "", last_used_at: null },
      ],
      team_requests: [{ toolkit_slug: "linear", requested_by: ["u2"], asked_by: ["u2"], agent_ids: ["a2"], last_requested_at: "" }],
      personal: [
        { id: "p-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null },
        { id: "p-old", toolkit_slug: "slack", status: "revoked", connected_at: "", last_used_at: null },
      ],
    };
    vi.mocked(api.listAppRequests).mockResolvedValue({ requests: [connectRequest({})] } as never);
    vi.mocked(api.getPersonalAppImpact).mockResolvedValue({
      agents: [{ id: "a1", name: "Support", workspace_id: "ws-1", workspace_name: "Acme", running: true }],
    });
    vi.mocked(api.deleteComposioConnection).mockResolvedValue(undefined as never);
  });

  it("opens from the URL with the team's accounts, then the viewer's, needs-action first", async () => {
    renderPage();
    const team = await screen.findByRole("region", { name: "Team accounts" });
    await within(team).findByText("Slack");
    expect(within(team).getAllByText(/^(Notion|Linear|Slack)$/).map((el) => el.textContent)).toEqual(["Notion", "Linear", "Slack"]);
    expect(screen.getByRole("tab", { name: /Accounts/ })).toHaveTextContent("5");
    expect(within(row("team", "notion")).getAllByText("Needs reconnecting").length).toBeGreaterThan(0);
    expect(within(row("team", "linear")).getAllByText(/Requested · admins notified/).length).toBeGreaterThan(0);
    expect(row("team", "linear")).toHaveTextContent("Name a2 needs it");
    // A member asks an admin; nothing connects an account apart from an app.
    expect(within(row("team", "notion")).getByRole("button", { name: "Ask an admin" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Connect (team )?account/ })).not.toBeInTheDocument();

    const yours = screen.getByRole("region", { name: "Your accounts" });
    expect(row("personal", "gmail")).toHaveTextContent("used by Name a1");
    await waitFor(() => expect(row("personal", "linear")).toHaveTextContent("Name a1 needs it"));
    expect(within(row("personal", "linear")).getByRole("button", { name: "Connect" })).toBeInTheDocument();
    expect(within(yours).queryByText("Slack")).not.toBeInTheDocument();
  });

  it("names only agents that act through the viewer's account under Your accounts", async () => {
    overview.agent_apps = [
      ...overview.agent_apps,
      { agent_id: "shared", toolkit_slug: "gmail", account_scope: "personal", level: "write" },
      { agent_id: "others-private", toolkit_slug: "gmail", account_scope: "personal", level: "write" },
      { agent_id: "archived", toolkit_slug: "youtube", account_scope: "personal", level: "write" },
    ];
    vi.mocked(api.listAgents).mockResolvedValue([
      { id: "a1", owner_id: "me", permission_mode: "private", invocation_targets: [], archived_at: null },
      { id: "a2", owner_id: "u2", permission_mode: "private", invocation_targets: [], archived_at: null },
      { id: "shared", owner_id: "u2", permission_mode: "public_to", invocation_targets: [{ target_type: "workspace", target_id: "ws-1" }], archived_at: null },
      { id: "others-private", owner_id: "u2", permission_mode: "private", invocation_targets: [], archived_at: null },
      { id: "archived", owner_id: "u2", permission_mode: "private", invocation_targets: [], archived_at: "2026-11-01T00:00:00Z" },
    ] as never);
    renderPage();
    await waitFor(() => expect(row("personal", "gmail")).toHaveTextContent("used by Name a1, Name shared"));
    expect(row("personal", "gmail")).not.toHaveTextContent("others-private");
    expect(row("personal", "youtube")).toBeNull();
    expect(screen.getByRole("tab", { name: /Accounts/ })).toHaveTextContent("5");
  });

  it("gives an admin Connect and Decline on a requested team account", async () => {
    overview.can_manage = true;
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("region", { name: "Team accounts" }).then((team) => within(team).findByText("Linear"));
    expect(within(row("team", "linear")).getAllByText(/Requested · Name a2, Name u2/).length).toBeGreaterThan(0);
    expect(within(row("team", "notion")).getByRole("button", { name: "Reconnect" })).toBeInTheDocument();
    await user.click(within(row("team", "linear")).getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(api.dismissTeamAppRequests).toHaveBeenCalledWith("linear"));
  });

  it("lets a member withdraw their own ask", async () => {
    overview.team_requests = [{ toolkit_slug: "linear", requested_by: ["me"], asked_by: ["me"], agent_ids: [], last_requested_at: "" }];
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("region", { name: "Team accounts" }).then((team) => within(team).findByText("Linear"));
    await user.click(within(row("team", "linear")).getByRole("button", { name: "Withdraw" }));
    await waitFor(() => expect(api.dismissTeamAppRequests).toHaveBeenCalledWith("linear"));
  });

  it("disconnects after listing the agents that lose the account", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText("Gmail");
    await user.click(within(row("personal", "gmail")).getByRole("button", { name: "Gmail actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Disconnect" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Disconnect Gmail?" });
    await waitFor(() => expect(within(dialog).getByText("Support")).toBeInTheDocument());
    await user.click(within(dialog).getByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(api.deleteComposioConnection).toHaveBeenCalledWith("p-gmail"));
  });

  it("filters accounts by search and keeps the tab in the URL", async () => {
    nav.search = "";
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("tab", { name: /Accounts/ }));
    expect(replace).toHaveBeenLastCalledWith("/acme/apps?tab=accounts");
    await user.type(screen.getByRole("searchbox", { name: "Search apps" }), "not");
    const team = screen.getByRole("region", { name: "Team accounts" });
    expect(within(team).getByText("Notion")).toBeInTheDocument();
    expect(within(team).queryByText("Slack")).not.toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Your accounts" })).getByText("You haven't connected any accounts in this workspace.")).toBeInTheDocument();
  });

  it("follows the URL when it changes outside the tabs, e.g. the sidebar link or Back", async () => {
    const { navigate } = renderPage();
    expect(await screen.findByRole("region", { name: "Team accounts" })).toBeInTheDocument();
    navigate("");
    await waitFor(() => expect(screen.getByRole("tab", { name: /In use/ })).toHaveAttribute("aria-selected", "true"));
    expect(screen.queryByRole("region", { name: "Team accounts" })).not.toBeInTheDocument();
    navigate("tab=accounts");
    expect(await screen.findByRole("region", { name: "Team accounts" })).toBeInTheDocument();
  });
});
