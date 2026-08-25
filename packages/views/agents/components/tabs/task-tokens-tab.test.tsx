// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { createQueryClient } from "@multica/core/query-client";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() },
}));

const mockOpenExternal = vi.hoisted(() => vi.fn());
vi.mock("../../../platform", () => ({ openExternal: mockOpenExternal }));

import { TaskTokensTab } from "./task-tokens-tab";

const AGENT = { id: "a-1", name: "Test Agent" } as never;

const CATALOG = {
  agent_id: "a-1",
  available: [
    { id: "erp", label: "ERP", description: "erp.example.com", env: "BOT_TOKEN_ERP" },
    { id: "app", label: "APP", description: "", env: "BOT_TOKEN_APP" },
  ],
  enabled: ["erp"],
};

function installApi(overrides: Record<string, unknown> = {}) {
  const getAgentTaskTokens = vi.fn().mockResolvedValue(CATALOG);
  const updateAgentTaskTokens = vi.fn().mockResolvedValue({
    ...CATALOG,
    enabled: ["erp", "app"],
  });
  setApiInstance({
    getAgentTaskTokens,
    updateAgentTaskTokens,
    ...overrides,
  } as unknown as ApiClient);
  return { getAgentTaskTokens, updateAgentTaskTokens };
}

function renderTab() {
  const queryClient = createQueryClient();
  render(
    <I18nProvider
      locale="en"
      resources={{ en: { agents: enAgents, common: enCommon } }}
    >
      <QueryClientProvider client={queryClient}>
        <TaskTokensTab agent={AGENT} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return queryClient;
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  cleanup();
});

describe("TaskTokensTab", () => {
  it("renders each catalog entry with the env it injects", async () => {
    installApi();
    renderTab();

    expect(await screen.findByText("ERP")).toBeInTheDocument();
    expect(screen.getByText("APP")).toBeInTheDocument();
    expect(screen.getByText(/BOT_TOKEN_ERP/)).toBeInTheDocument();

    const erp = screen.getByRole("checkbox", { name: /ERP/ });
    await waitFor(() => expect(erp).toBeChecked());
    expect(screen.getByRole("checkbox", { name: /APP/ })).not.toBeChecked();
  });

  it("sends the full enabled set when a box is ticked", async () => {
    const { updateAgentTaskTokens } = installApi();
    renderTab();

    await userEvent.click(await screen.findByRole("checkbox", { name: /APP/ }));

    await waitFor(() => {
      expect(updateAgentTaskTokens).toHaveBeenCalledWith("a-1", ["erp", "app"]);
    });
  });

  it("links to the integration guide on the docs site", async () => {
    installApi();
    renderTab();

    const link = await screen.findByTestId("task-tokens-docs-link");
    await userEvent.click(link);

    // The docs site is a separate app at multica.ai/docs, not a route on this
    // deployment, so the link is absolute like every other doc link.
    expect(mockOpenExternal).toHaveBeenCalledWith(
      "https://multica.ai/docs/task-identity-tokens",
    );
  });

  it("shows the unconfigured notice when the catalog is empty", async () => {
    installApi({
      getAgentTaskTokens: vi
        .fn()
        .mockResolvedValue({ agent_id: "a-1", available: [], enabled: [] }),
    });
    renderTab();

    expect(
      await screen.findByText(/no identity tokens configured/i),
    ).toBeInTheDocument();
  });
});
