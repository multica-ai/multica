import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentApp } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enSettings from "../../../locales/en/settings.json";
import { AgentAppsTab } from "./agent-apps-tab";

vi.mock("@multica/core/api", () => ({ api: {
  listAgentApps: vi.fn(), getAppsOverview: vi.fn(), listAppCatalog: vi.fn(), updateAgentApps: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ settings: () => "/ws/settings" }) }));
vi.mock("../../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));

const apps: AgentApp[] = [
  { agent_id: "a1", toolkit_slug: "gmail", account_scope: "personal", level: "read" },
];

beforeEach(() => {
  vi.mocked(api.listAgentApps).mockResolvedValue({ apps });
  vi.mocked(api.getAppsOverview).mockResolvedValue({
    catalog_mode: "all", can_manage: true, team: [], personal: [], agent_apps: apps,
  });
  vi.mocked(api.listAppCatalog).mockResolvedValue({
    catalog_mode: "all",
    apps: [
      { slug: "gmail", name: "Gmail", curated: true },
      { slug: "notion", name: "Notion", curated: true },
    ],
  });
  vi.mocked(api.updateAgentApps).mockResolvedValue({ apps });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderApps() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, agents: enAgents, settings: enSettings } }}>
        <AgentAppsTab agent={{ id: "a1", permission_mode: "private" } as Agent} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("AgentAppsTab", () => {
  it("assigns apps and accounts without a permission control, preserving existing grants", async () => {
    const user = userEvent.setup();
    renderApps();
    await screen.findByText("Gmail");
    expect(screen.queryByRole("combobox", { name: "Level" })).not.toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Account" })).toBeVisible();
    await user.click(screen.getByRole("combobox", { name: "Add an app…" }));
    await user.click(await screen.findByRole("option", { name: "Notion" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", {
      apps: [
        { toolkit_slug: "gmail", account_scope: "personal", level: "read" },
        { toolkit_slug: "notion", account_scope: "personal", level: "write" },
      ],
    }));
  });

  it("revokes access by removing the app", async () => {
    const user = userEvent.setup();
    renderApps();
    await user.click(await screen.findByRole("button", { name: "Remove Gmail" }));
    await waitFor(() => expect(api.updateAgentApps).toHaveBeenCalledWith("a1", { apps: [] }));
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
