import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentApp, AppsOverview } from "@multica/core/types";
import { getTestResources } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { AgentAppsSection } from "./agent-apps-section";

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));
vi.mock("@multica/core/api", () => ({ api: {
  listAgentApps: vi.fn(), getAppsOverview: vi.fn(), listAppCatalog: vi.fn(),
  updateAgentApps: vi.fn(), requestTeamApp: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Ops Assistant" }) }));
vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: { id: "me" } });
  return { useAuthStore: Object.assign((sel: (s: ReturnType<typeof state>) => unknown) => sel(state()), { getState: state }) };
});

const agent = { id: "a1", name: "Ops Assistant" } as Agent;
let apps: AgentApp[];
let overview: AppsOverview;
const active = (slug: string) => ({ id: `c-${slug}`, toolkit_slug: slug, status: "active", connected_at: "", last_used_at: null });

beforeEach(() => {
  apps = [
    { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "write" },
    { agent_id: "a1", toolkit_slug: "googlecalendar", account_scope: "personal", level: "write" },
    { agent_id: "a1", toolkit_slug: "sentry", account_scope: "team", level: "read" },
    { agent_id: "a1", toolkit_slug: "posthog", account_scope: "team", level: "write" },
  ];
  overview = {
    catalog_mode: "all", can_manage: false,
    team: [active("sentry"), active("slack")],
    personal: [active("gmail")],
    agent_apps: apps,
  };
  vi.mocked(api.listAgentApps).mockImplementation(async () => ({ apps }));
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({
    catalog_mode: "all",
    apps: [
      { slug: "slack", name: "Slack", curated: true, recommended_scope: "team" },
      { slug: "linear", name: "Linear", curated: true, category: "Issues" },
      { slug: "gmail", name: "Gmail", curated: true },
      { slug: "googlecalendar", name: "Google Calendar", curated: true },
      { slug: "sentry", name: "Sentry", curated: true },
      { slug: "posthog", name: "PostHog", curated: false },
      { slug: "hidden", name: "Hidden", curated: false, allowed: false },
    ],
  });
  vi.mocked(api.updateAgentApps).mockImplementation(async (_id, body) => ({ apps: body.apps.map((app) => ({ ...app, agent_id: "a1" })) }));
  vi.mocked(api.requestTeamApp).mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection({ canEdit = true }: { canEdit?: boolean } = {}) {
  const onManage = vi.fn();
  const navigation: NavigationAdapter = {
    push: vi.fn(), replace: vi.fn(), back: vi.fn(),
    pathname: "/ws/agents/a1", searchParams: new URLSearchParams(), hash: "",
    getShareableUrl: (p) => p,
  };
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider locale="en" resources={getTestResources()}>
        <NavigationProvider value={navigation}>
          <AgentAppsSection agent={agent} canEdit={canEdit} onManage={onManage} />
        </NavigationProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onManage };
}

const row = (scope: "team" | "personal", slug: string) =>
  document.querySelector(`[data-account-row="${scope}:${slug}"]`) as HTMLElement;

describe("AgentAppsSection", () => {
  it("says whose account each app acts as and where it stands, with one action each", async () => {
    renderSection();
    await screen.findByText("Google Calendar");
    expect(row("personal", "gmail")).toHaveTextContent("Acts as whoever starts the run");
    expect(row("personal", "gmail")).toHaveTextContent("Connected");
    expect(within(row("personal", "gmail")).queryByRole("button", { name: /Connect|Reconnect/ })).not.toBeInTheDocument();
    expect(row("personal", "googlecalendar")).toHaveTextContent("Not connected");
    expect(within(row("personal", "googlecalendar")).getByRole("button", { name: "Connect" })).toBeVisible();
    expect(row("team", "sentry")).toHaveTextContent("Acts as the team account");
    expect(within(row("team", "sentry")).queryByRole("button")).not.toBeInTheDocument();
    expect(row("team", "posthog")).toHaveTextContent("Not connected");
    expect(within(row("team", "posthog")).getByRole("button", { name: "Ask an admin" })).toBeVisible();
  });

  it("opens the pre-connect notice for the viewer's own account", async () => {
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("Google Calendar");
    await user.click(within(row("personal", "googlecalendar")).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog", { name: "Connect Google Calendar" });
    expect(dialog).toHaveTextContent("Your own account");
    expect(api.requestTeamApp).not.toHaveBeenCalled();
  });

  it("asks an admin for a team account on behalf of this agent, then lets the asker withdraw", async () => {
    vi.mocked(api.requestTeamApp).mockImplementation(async () => {
      overview.team_requests = [{ toolkit_slug: "posthog", requested_by: ["me"], asked_by: ["me"], agent_ids: ["a1"], last_requested_at: "" }];
    });
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("PostHog");
    await user.click(within(row("team", "posthog")).getByRole("button", { name: "Ask an admin" }));
    expect(api.requestTeamApp).toHaveBeenCalledWith("posthog", "a1");
    await waitFor(() => expect(row("team", "posthog")).toHaveTextContent("Requested · admin notified"));
    expect(within(row("team", "posthog")).getByRole("button", { name: "Withdraw" })).toBeVisible();
    expect(toast.success).toHaveBeenCalled();
  });

  it("offers admins the team account instead of a request", async () => {
    overview.can_manage = true;
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("PostHog");
    await user.click(within(row("team", "posthog")).getByRole("button", { name: "Connect" }));
    expect(await screen.findByRole("dialog", { name: "Connect PostHog" })).toHaveTextContent("A team account the whole workspace shares");
  });

  it("is read-only for someone who cannot edit the agent", async () => {
    renderSection({ canEdit: false });
    await screen.findByText("Google Calendar");
    expect(screen.queryByRole("button", { name: "Manage" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add" })).not.toBeInTheDocument();
    expect(within(row("personal", "googlecalendar")).getByRole("button", { name: "Connect" })).toBeVisible();
  });

  it("offers Add from the empty state", async () => {
    apps = [];
    renderSection();
    expect(await screen.findByText("This agent doesn't use any apps yet.")).toBeVisible();
    expect(screen.getAllByRole("button", { name: "Add" })).toHaveLength(2);
  });

  it("opens advanced settings from Manage", async () => {
    const user = userEvent.setup();
    const { onManage } = renderSection();
    await screen.findByText("Gmail");
    await user.click(screen.getByRole("button", { name: "Manage" }));
    expect(onManage).toHaveBeenCalledTimes(1);
  });
});

describe("AddAgentAppDialog", () => {
  it("lists the catalog apps not on the agent and adds one, keeping the others", async () => {
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("Gmail");
    await user.click(screen.getByRole("button", { name: "Add" }));
    const dialog = await screen.findByRole("dialog", { name: "Add an app to Ops Assistant" });
    expect(within(dialog).getByPlaceholderText("Search 2 apps")).toBeVisible();
    expect(within(dialog).queryByText("Gmail")).not.toBeInTheDocument();
    expect(within(dialog).queryByText("Hidden")).not.toBeInTheDocument();
    expect(within(dialog).getByText("Linear").closest("li")).toHaveTextContent("Issues · Not connected");

    await user.click(within(dialog).getByRole("button", { name: "Add Linear" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "write" },
        { toolkit_slug: "googlecalendar", account_scope: "personal", level: "write" },
        { toolkit_slug: "sentry", account_scope: "team", level: "read" },
        { toolkit_slug: "posthog", account_scope: "team", level: "write" },
        { toolkit_slug: "linear", account_scope: "personal", level: "write" },
      ],
    }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(toast.success).toHaveBeenCalledWith("Added Linear to Ops Assistant.");
  });

  it("gives an admin's add the team account when the app is usually a team app", async () => {
    overview.can_manage = true;
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("Gmail");
    await user.click(screen.getByRole("button", { name: "Add" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Slack").closest("li")).toHaveTextContent("Usually a team account · Team account connected");
    await user.click(within(dialog).getByRole("button", { name: "Add Slack" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: expect.arrayContaining([{ toolkit_slug: "slack", account_scope: "team", level: "write" }]),
    }));
  });

  it("stays open with an error when saving fails", async () => {
    vi.mocked(api.updateAgentApps).mockRejectedValueOnce(new Error("offline"));
    const user = userEvent.setup();
    renderSection();
    await screen.findByText("Gmail");
    await user.click(screen.getByRole("button", { name: "Add" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Add Linear" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("offline"));
    expect(screen.getByRole("dialog")).toBeVisible();
  });
});
