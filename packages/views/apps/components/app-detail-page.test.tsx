import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AppsOverview, MemberRole } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AppDetailPage } from "./app-detail-page";

vi.mock("@multica/core/api", () => ({ ApiError: class ApiError extends Error {}, api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), listAgents: vi.fn(), updateAgentApps: vi.fn(),
  listAppActivity: vi.fn(), getIssue: vi.fn(), requestTeamApp: vi.fn(), dismissTeamAppRequests: vi.fn(),
  connectApp: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({
    apps: () => "/acme/apps",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => `Name ${id}` }),
}));
const viewer = vi.hoisted(() => ({ role: "member" as MemberRole }));
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useCurrentMember: () => ({ userId: "me", role: viewer.role, member: null, isLoading: false }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "me" } };
  return { useAuthStore: (selector: (s: typeof state) => unknown) => selector(state) };
});
vi.mock("../../common/use-composio-available", () => ({ useComposioAvailable: () => true }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

let overview: AppsOverview;

beforeEach(() => {
  viewer.role = "member";
  overview = {
    catalog_mode: "all", can_manage: false, personal: [],
    team: [{ id: "t-notion", toolkit_slug: "notion", status: "active", connected_at: "", last_used_at: null }],
    agent_apps: [
      { agent_id: "mine", toolkit_slug: "gmail", account_scope: "personal", level: "read" },
      { agent_id: "mine", toolkit_slug: "notion", account_scope: "personal", level: "write" },
      { agent_id: "shared", toolkit_slug: "notion", account_scope: "team", level: "write" },
    ],
  };
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "all", apps: [
    { slug: "notion", name: "Notion", category: "Docs", curated: true },
  ] });
  vi.mocked(api.listAgents).mockResolvedValue([
    { id: "mine", name: "Mine", owner_id: "me", archived_at: null },
    { id: "shared", name: "Shared", owner_id: "me", archived_at: null },
    { id: "other", name: "Other", owner_id: "u2", archived_at: null },
    { id: "gone", name: "Gone", owner_id: "me", archived_at: "2026-01-01" },
  ] as never);
  vi.mocked(api.listAppActivity).mockImplementation(async (_slug, scope) => ({
    calls: scope === "personal"
      ? [{ id: "c1", agent_id: "mine", issue_id: "i1", chat_session_id: null, triggered_by: "me",
          tool_slug: "NOTION_CREATE_PAGE", action_class: "write", outcome: "ok", error: null,
          created_at: "2026-11-17T10:00:00Z" }]
      : [{ id: "c2", agent_id: "shared", issue_id: null, chat_session_id: "s1", triggered_by: "u2",
          tool_slug: "NOTION_SEARCH", action_class: "read", outcome: "ok", error: null,
          created_at: "2026-11-17T11:00:00Z" }],
  }) as never);
  vi.mocked(api.getIssue).mockResolvedValue({ id: "i1", identifier: "MUL-7" } as never);
  vi.mocked(api.updateAgentApps).mockImplementation(async (agentId, body) => {
    overview = {
      ...overview,
      agent_apps: [
        ...overview.agent_apps.filter((a) => a.agent_id !== agentId),
        ...body.apps.map((a) => ({ agent_id: agentId, toolkit_slug: a.toolkit_slug, account_scope: a.account_scope, level: a.level ?? "write" })),
      ],
    };
    return { apps: overview.agent_apps.filter((a) => a.agent_id === agentId) };
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  const adapter: NavigationAdapter = {
    push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/apps/notion",
    searchParams: new URLSearchParams(), hash: "", getShareableUrl: (p) => p,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <AppDetailPage toolkitSlug="notion" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

const agentSwitch = (agent: string) => screen.getByRole("switch", { name: `Let ${agent} use Notion` });

describe("AppDetailPage", () => {
  it("lists live agents with whose account each uses", async () => {
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).toBeChecked());
    expect(agentSwitch("Shared")).toBeChecked();
    expect(agentSwitch("Other")).not.toBeChecked();
    expect(screen.queryByText("Gone")).not.toBeInTheDocument();
    expect(screen.getByText("Not using Notion")).toBeInTheDocument();
  });

  it("saves an agent's whole app list when it stops using the app", async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(agentSwitch("Mine"));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("mine", {
      apps: [{ toolkit_slug: "gmail", account_scope: "personal", level: "read" }],
    }));
    await waitFor(() => expect(agentSwitch("Mine")).not.toBeChecked());
  });

  it("shows the new value before the save lands and keeps it after", async () => {
    let finish!: () => void;
    const saved = new Promise<void>((resolve) => { finish = resolve; });
    const save = vi.mocked(api.updateAgentApps).getMockImplementation()!;
    vi.mocked(api.updateAgentApps).mockImplementation(async (agentId, body) => {
      await saved;
      return save(agentId, body);
    });
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(agentSwitch("Mine"));
    expect(agentSwitch("Mine")).not.toBeChecked();
    expect(agentSwitch("Mine")).toHaveAttribute("aria-disabled", "true");
    finish();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    expect(agentSwitch("Mine")).not.toBeChecked();
  });

  it("puts the switch back when the save fails", async () => {
    vi.mocked(api.updateAgentApps).mockRejectedValue(new Error("nope"));
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(agentSwitch("Mine"));
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    expect(agentSwitch("Mine")).toBeChecked();
  });

  it("gives a member's agent the triggerer's account when enabled", async () => {
    overview.agent_apps = overview.agent_apps.filter((a) => a.agent_id !== "mine" || a.toolkit_slug !== "notion");
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(agentSwitch("Mine"));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("mine", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "notion", account_scope: "personal", level: "write" },
      ],
    }));
  });

  it("keeps members off team-account agents and agents they can't edit", async () => {
    renderPage();
    await waitFor(() => expect(agentSwitch("Mine")).not.toHaveAttribute("aria-disabled", "true"));
    expect(agentSwitch("Shared")).toHaveAttribute("aria-disabled", "true");
    expect(agentSwitch("Other")).toHaveAttribute("aria-disabled", "true");
  });

  it("lets an admin change any agent", async () => {
    viewer.role = "admin";
    overview.can_manage = true;
    renderPage();
    await waitFor(() => expect(agentSwitch("Shared")).not.toHaveAttribute("aria-disabled", "true"));
    expect(agentSwitch("Other")).not.toHaveAttribute("aria-disabled", "true");
  });

  it("shows members only their own activity, and admins the team's too", async () => {
    const first = renderPage();
    expect(await screen.findByText("Create page")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("link", { name: "MUL-7" })).toHaveAttribute("href", "/acme/issues/MUL-7"));
    expect(screen.queryByText("Search")).not.toBeInTheDocument();
    expect(api.listAppActivity).toHaveBeenCalledTimes(1);
    first.unmount();

    overview.can_manage = true;
    renderPage();
    expect(await screen.findByText("Search")).toBeInTheDocument();
    const rows = screen.getAllByText(/^(Search|Create page)$/).map((el) => el.textContent);
    expect(rows).toEqual(["Search", "Create page"]);
  });

  it("offers a member to ask for a team account the workspace lacks", async () => {
    overview.team = [];
    vi.mocked(api.requestTeamApp).mockResolvedValue(undefined as never);
    const user = userEvent.setup();
    renderPage();
    await user.click(await screen.findByRole("button", { name: "Ask an admin" }));
    await waitFor(() => expect(api.requestTeamApp).toHaveBeenCalledWith("notion", undefined));
  });

  it("shows admins who asked for the team account and lets them decline it", async () => {
    overview = {
      ...overview, can_manage: true, team: [],
      team_requests: [{ toolkit_slug: "notion", requested_by: ["u2", "u3"], agent_ids: [], last_requested_at: "" }],
    };
    vi.mocked(api.dismissTeamAppRequests).mockResolvedValue(undefined as never);
    const user = userEvent.setup();
    renderPage();
    await screen.findAllByText("Requested · Name u2, Name u3");
    const team = document.querySelector('[data-account-row="team:notion"]') as HTMLElement;
    expect(within(team).getByRole("button", { name: "Connect" })).toBeInTheDocument();
    await user.click(within(team).getByRole("button", { name: "Decline" }));
    await waitFor(() => expect(api.dismissTeamAppRequests).toHaveBeenCalledWith("notion"));
  });
});
