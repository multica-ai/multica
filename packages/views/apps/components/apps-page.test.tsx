import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { AppsOverview } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AppsPage } from "./apps-page";

vi.mock("@multica/core/api", () => ({ api: {
  getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), listAgents: vi.fn(),
  updateAppSettings: vi.fn(), requestTeamApp: vi.fn(), connectApp: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({ apps: () => "/acme/apps", appDetail: (slug: string) => `/acme/apps/${slug}` }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => `Name ${id}` }),
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
    { id: "a1", name: "Support", archived_at: null },
    { id: "a2", name: "Triage", archived_at: null },
  ] as never);
  vi.mocked(api.requestTeamApp).mockResolvedValue(undefined as never);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPage() {
  const adapter: NavigationAdapter = {
    push: vi.fn(), replace, back: vi.fn(), pathname: "/acme/apps",
    searchParams: new URLSearchParams(nav.search), hash: "", getShareableUrl: (p) => p,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <AppsPage />
      </NavigationProvider>
    </QueryClientProvider>,
  );
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
    await waitFor(() => expect(screen.queryByText("A saved app list is hiding other apps.")).not.toBeInTheDocument());
  });

  it("shows members the saved catalog without a way to lift it", async () => {
    overview = { ...overview, catalog_mode: "custom", allowed_toolkits: [] };
    renderPage();
    expect(await screen.findByText("A saved app list is hiding other apps.")).toBeInTheDocument();
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
