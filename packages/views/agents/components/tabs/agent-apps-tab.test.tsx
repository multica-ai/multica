import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { toast } from "sonner";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentApp, AppsOverview } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enSettings from "../../../locales/en/settings.json";
import enBilling from "../../../locales/en/billing.json";
import { AgentAppsTab } from "./agent-apps-tab";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: {
    listAgentApps: vi.fn(), getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), updateAgentApps: vi.fn(),
    getWorkspaceFeatures: vi.fn(), listAgents: vi.fn(), listMembers: vi.fn(),
  },
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "me" } };
  return { useAuthStore: (selector: (s: typeof state) => unknown) => selector(state) };
});
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/ws/settings", apps: () => "/ws/apps" }) }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Agent" }) }));
vi.mock("../../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ pathname: "/ws/agents/a1", searchParams: new URLSearchParams(), hash: "" }),
  currentPath: () => "/ws/agents/a1",
}));

const apps: AgentApp[] = [
  { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "read" },
];
let overview: AppsOverview;

beforeEach(() => {
  vi.mocked(api.listAgentApps).mockResolvedValue({ apps });
  overview = {
    catalog_mode: "all", can_manage: true,
    team: [{ id: "team-notion", toolkit_slug: "notion", status: "active", connected_at: "", last_used_at: null }],
    personal: [], agent_apps: apps,
  };
  vi.mocked(api.getAppsOverview).mockImplementation(async () => overview);
  vi.mocked(api.listAppCatalog).mockResolvedValue({
    catalog_mode: "all",
    apps: [
      { slug: "gmail", name: "Gmail", curated: true },
      { slug: "notion", name: "Notion", curated: true },
      { slug: "slack", name: "Slack", curated: true, recommended_scope: "team" },
      { slug: "googlecalendar", name: "Google Calendar", curated: true },
    ],
  });
  vi.mocked(api.updateAgentApps).mockImplementation(async (_id, body) => ({ apps: body.apps.map((app) => ({ ...app, agent_id: "a1" })) }));
  vi.mocked(api.getWorkspaceFeatures).mockResolvedValue({ features: {} });
  vi.mocked(api.listAgents).mockResolvedValue([{ id: "a1", owner_id: "me", permission_mode: "private", invocation_targets: [], archived_at: null }] as never);
  vi.mocked(api.listMembers).mockResolvedValue([{ user_id: "me", role: "member" }] as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderApps() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, agents: enAgents, settings: enSettings, billing: enBilling } }}>
        <AgentAppsTab agent={{ id: "a1", permission_mode: "private" } as Agent} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("AgentAppsTab", () => {
  it("lists connected apps and saved grants with icons, excluding unconnected catalog entries", async () => {
    overview.personal = [{ id: "expired-calendar", toolkit_slug: "googlecalendar", status: "expired", connected_at: "", last_used_at: null }];
    renderApps();
    await screen.findByText("Gmail");
    expect(screen.getByText(/Not connected/)).toBeVisible();
    expect(screen.getByRole("switch", { name: "Enable Gmail" })).toBeChecked();
    expect(screen.getByRole("switch", { name: "Enable Notion" })).not.toBeChecked();
    expect(screen.getByText("Notion").closest("li")?.querySelector("img")).toHaveAttribute("src", expect.stringContaining("notion"));
    expect(screen.queryByText("Google Calendar")).not.toBeInTheDocument();
    expect(screen.queryByText("Slack")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("enables the only connected source and preserves existing grants", async () => {
    const user = userEvent.setup();
    renderApps();
    await screen.findByText("Gmail");
    expect(screen.queryByRole("combobox", { name: "Account scope" })).not.toBeInTheDocument();
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    await user.click(screen.getByRole("switch", { name: "Enable Notion" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "notion", account_scope: "team", level: "write" },
      ],
    }));
  });

  it("revokes access by removing the app", async () => {
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("switch", { name: "Enable Gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", { apps: [] }));
  });

  it.each([
    { team: true, personal: false, scope: "team" },
    { team: false, personal: true, scope: "personal" },
    { team: true, personal: true, scope: "team" },
  ])("uses an available default source ($scope) for a new app", async ({ team, personal, scope }) => {
    const account = { id: "slack", toolkit_slug: "slack", status: "active", connected_at: "", last_used_at: null };
    if (team) overview.team.push(account);
    if (personal) overview.personal.push(account);
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("switch", { name: "Enable Slack" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "slack", account_scope: scope, level: "write" },
      ],
    }));
  });

  it("changes a dual-account app's source without widening its saved read-only grant", async () => {
    overview.team.push({ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    overview.personal.push({ id: "my-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    const user = userEvent.setup();
    renderApps();
    const sources = await screen.findByRole("radiogroup", { name: "Account for Gmail" });
    const personal = within(sources).getByRole("radio", { name: "Starter's account" });
    expect(personal).toBeChecked();
    personal.focus();
    await user.keyboard("{ArrowLeft}");
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [{ toolkit_slug: "gmail", account_scope: "team", level: "read" }],
    }));
  });

  it("locks team grants for members while allowing their personal apps", async () => {
    overview.can_manage = false;
    overview.team.push({ id: "team-slack", toolkit_slug: "slack", status: "active", connected_at: "", last_used_at: null });
    overview.personal.push({ id: "my-slack", toolkit_slug: "slack", status: "active", connected_at: "", last_used_at: null });
    vi.mocked(api.listAgentApps).mockResolvedValue({ apps: [...apps, { agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "read" }] });
    const user = userEvent.setup();
    renderApps();
    expect(await screen.findByRole("switch", { name: "Enable Notion" })).toHaveAttribute("aria-disabled", "true");
    await user.click(screen.getByRole("switch", { name: "Enable Slack" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "notion", account_scope: "team", level: "read" },
        { toolkit_slug: "slack", account_scope: "personal", level: "write" },
      ],
    }));
    const sources = await screen.findByRole("radiogroup", { name: "Account for Slack" });
    expect(within(sources).getByRole("radio", { name: "Team account" })).toHaveAttribute("aria-disabled", "true");
    expect(api.updateAgentApps).toHaveBeenCalledTimes(1);
  });

  it("adds a team-only app for a member as a personal-account app", async () => {
    overview.can_manage = false;
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("switch", { name: "Enable Notion" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "notion", account_scope: "personal", level: "write" },
      ],
    }));
  });

  it("connects a missing account through the pre-connect notice", async () => {
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("button", { name: "Connect yours" }));
    expect(await screen.findByRole("dialog", { name: "Connect Gmail" })).toBeVisible();
  });

  it("retains saved apps missing from the catalog and allows removing them", async () => {
    vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "custom", apps: [] });
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("switch", { name: "Enable gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", { apps: [] }));
  });

  it.each(["loading", "failed"])("keeps saved grants editable and new grants disabled while the catalog is %s", async (state) => {
    if (state === "loading") vi.mocked(api.listAppCatalog).mockReturnValue(new Promise(() => {}));
    else vi.mocked(api.listAppCatalog).mockRejectedValue(new Error("offline"));
    overview.team.push({ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    overview.personal.push({ id: "my-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    const user = userEvent.setup();
    renderApps();
    const sources = await screen.findByRole("radiogroup", { name: "Account for gmail" });
    expect(screen.getByText("gmail").closest("li")).toHaveTextContent("G");
    const newGrant = screen.getByRole("switch", { name: "Enable notion" });
    expect(newGrant).toHaveAttribute("aria-disabled", "true");
    await user.click(newGrant);
    expect(api.updateAgentApps).not.toHaveBeenCalled();
    await user.click(within(sources).getByRole("radio", { name: "Team account" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [{ toolkit_slug: "gmail", account_scope: "team", level: "read" }],
    }));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Enable gmail" })).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(screen.getByRole("switch", { name: "Enable gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenLastCalledWith("a1", { apps: [] }));
  });

  it("keeps team grants locked for members when the catalog fails", async () => {
    overview.can_manage = false;
    overview.personal.push({ id: "my-notion", toolkit_slug: "notion", status: "active", connected_at: "", last_used_at: null });
    vi.mocked(api.listAgentApps).mockResolvedValue({ apps: [{ agent_id: "a1", toolkit_slug: "notion", account_scope: "team", level: "read" }] });
    vi.mocked(api.listAppCatalog).mockRejectedValue(new Error("offline"));
    const user = userEvent.setup();
    renderApps();
    const toggle = await screen.findByRole("switch", { name: "Enable notion" });
    expect(toggle).toHaveAttribute("aria-disabled", "true");
    await user.click(toggle);
    const sources = screen.getByRole("radiogroup", { name: "Account for notion" });
    for (const source of within(sources).getAllByRole("radio")) {
      expect(source).toHaveAttribute("aria-disabled", "true");
      await user.click(source);
    }
    expect(api.updateAgentApps).not.toHaveBeenCalled();
  });

  it("keeps known catalog restrictions on source changes while allowing revocation", async () => {
    overview.team.push({ id: "team-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    overview.personal.push({ id: "my-gmail", toolkit_slug: "gmail", status: "active", connected_at: "", last_used_at: null });
    vi.mocked(api.listAppCatalog).mockResolvedValue({ catalog_mode: "custom", apps: [{ slug: "gmail", name: "Gmail", curated: true, allowed: false }] });
    const user = userEvent.setup();
    renderApps();
    const sources = await screen.findByRole("radiogroup", { name: "Account for Gmail" });
    const team = within(sources).getByRole("radio", { name: "Team account" });
    expect(team).toHaveAttribute("aria-disabled", "true");
    await user.click(team);
    expect(api.updateAgentApps).not.toHaveBeenCalled();
    await user.click(screen.getByRole("switch", { name: "Enable Gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", { apps: [] }));
  });

  it("keeps the previous grant after a failed toggle and permits retry", async () => {
    vi.mocked(api.updateAgentApps).mockRejectedValueOnce(new Error("offline"));
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("switch", { name: "Enable Notion" }));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Enable Notion" })).not.toHaveAttribute("aria-disabled", "true"));
    expect(screen.getByRole("switch", { name: "Enable Notion" })).not.toBeChecked();
    await user.click(screen.getByRole("switch", { name: "Enable Notion" }));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Enable Notion" })).toBeChecked());
  });

  it("does not offer edits when account availability cannot be loaded", async () => {
    vi.mocked(api.getAppsOverview).mockRejectedValue(new Error("offline"));
    renderApps();
    expect(await screen.findByText("Couldn't load this agent's apps.")).toBeVisible();
    expect(screen.queryByRole("switch")).not.toBeInTheDocument();
  });

  it("explains saved read-only access and only replaces it after confirmation", async () => {
    const user = userEvent.setup();
    renderApps();
    expect(await screen.findByText("This app still has read-only access from an earlier setup.")).toBeVisible();
    expect(api.updateAgentApps).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Allow all actions" }));
    expect(api.updateAgentApps).not.toHaveBeenCalled();
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Allow all actions" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [{ toolkit_slug: "gmail", account_scope: "personal", level: "write" }],
    }));
  });
});

describe("AgentAppsTab on a plan without team app accounts (MUL-9491)", () => {
  const lock = () => vi.mocked(api.getWorkspaceFeatures).mockResolvedValue({ features: { team_app_accounts: "locked" } });
  const dual = (slug: string) => {
    overview.team.push({ id: `team-${slug}`, toolkit_slug: slug, status: "active", connected_at: "", last_used_at: null });
    overview.personal.push({ id: `my-${slug}`, toolkit_slug: slug, status: "active", connected_at: "", last_used_at: null });
  };

  it("gives a team-only app the triggerer's account, not the team account, and shows the upgrade notice", async () => {
    lock();
    const user = userEvent.setup();
    renderApps();
    expect(await screen.findByText("Not included in this workspace's plan.")).toBeVisible();
    await user.click(screen.getByRole("switch", { name: "Enable Notion" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: expect.arrayContaining([{ toolkit_slug: "notion", account_scope: "personal", level: "write" }]),
    }));
  });

  it("enables a dual-account app on the triggerer's account and cannot move it to the team account", async () => {
    lock();
    dual("slack");
    const user = userEvent.setup();
    renderApps();
    await screen.findByText("Not included in this workspace's plan.");
    await user.click(screen.getByRole("switch", { name: "Enable Slack" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "slack", account_scope: "personal", level: "write" },
      ],
    }));
    const sources = await screen.findByRole("radiogroup", { name: "Account for Slack" });
    const team = within(sources).getByRole("radio", { name: "Team account" });
    expect(team).toHaveAttribute("aria-disabled", "true");
    await user.click(team);
    expect(api.updateAgentApps).toHaveBeenCalledTimes(1);
  });

  it("keeps an existing team app: it can switch to the triggerer's account or be removed, but not be raised from read", async () => {
    lock();
    dual("gmail");
    vi.mocked(api.listAgentApps).mockResolvedValue({ apps: [{ agent_id: "a1", toolkit_slug: "gmail", account_scope: "team", level: "read" }] });
    const user = userEvent.setup();
    renderApps();
    // Once for the tab, once where "Allow all actions" would raise the level.
    expect(await screen.findAllByText("Read-only. Upgrade to edit.")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Allow all actions" })).not.toBeInTheDocument();
    const sources = screen.getByRole("radiogroup", { name: "Account for Gmail" });
    expect(within(sources).getByRole("radio", { name: "Team account" })).toBeChecked();
    await user.click(within(sources).getByRole("radio", { name: "Starter's account" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [{ toolkit_slug: "gmail", account_scope: "personal", level: "read" }],
    }));
    await user.click(screen.getByRole("switch", { name: "Enable Gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenLastCalledWith("a1", { apps: [] }));
  });

  it("explains a feature_locked refusal and refetches the plan", async () => {
    vi.mocked(api.updateAgentApps).mockRejectedValue(
      new ApiError("locked", 403, "Forbidden", { code: "feature_locked", feature: "team_app_accounts" }),
    );
    const user = userEvent.setup();
    renderApps();
    const toggle = await screen.findByRole("switch", { name: "Enable Notion" });
    await waitFor(() => expect(toggle).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(toggle);
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enBilling.paid_feature.locked_error));
    await waitFor(() => expect(api.getWorkspaceFeatures).toHaveBeenCalledTimes(2));
  });
});
