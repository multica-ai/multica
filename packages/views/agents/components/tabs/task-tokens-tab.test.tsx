// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  // Rapid toggling is the lost-write case: the endpoint replaces the whole
  // enabled set, so a second PUT computed from the pre-first-toggle snapshot
  // would silently undo the first one. On the surface that decides which
  // identities an agent may be issued, that must not happen quietly.
  it("keeps an earlier toggle when a second one follows immediately", async () => {
    const updateAgentTaskTokens = vi.fn(async (_id: string, next: string[]) => ({
      ...CATALOG,
      enabled: next,
    }));
    installApi({ updateAgentTaskTokens });
    renderTab();

    const app = await screen.findByRole("checkbox", { name: /APP/ });
    const erp = screen.getByRole("checkbox", { name: /ERP/ });
    await waitFor(() => expect(erp).toBeChecked());

    // Both clicks in one synchronous block: the component has not re-rendered
    // in between, which is exactly what a fast double-toggle looks like.
    fireEvent.click(app);
    fireEvent.click(erp);

    await waitFor(() => expect(updateAgentTaskTokens).toHaveBeenCalledTimes(2));
    expect(updateAgentTaskTokens.mock.calls[0]?.[1]).toEqual(["erp", "app"]);
    // ["app"], not []: the second payload is computed after the first landed.
    expect(updateAgentTaskTokens.mock.calls[1]?.[1]).toEqual(["app"]);

    await waitFor(() => expect(app).toBeChecked());
    expect(erp).not.toBeChecked();
  });

  it("locks every checkbox while a save is in flight", async () => {
    let release: (value: unknown) => void = () => {};
    const inFlight = new Promise((resolve) => {
      release = resolve;
    });
    const updateAgentTaskTokens = vi.fn(async (_id: string, next: string[]) => {
      await inFlight;
      return { ...CATALOG, enabled: next };
    });
    installApi({ updateAgentTaskTokens });
    renderTab();

    const app = await screen.findByRole("checkbox", { name: /APP/ });
    const erp = screen.getByRole("checkbox", { name: /ERP/ });
    fireEvent.click(app);

    // Not just the box being saved: any other box would compute its payload
    // from a set the server is in the middle of replacing. The checkbox is a
    // span with role="checkbox", so the disabled state is aria-disabled.
    await waitFor(() => expect(app).toHaveAttribute("aria-disabled", "true"));
    expect(erp).toHaveAttribute("aria-disabled", "true");

    release(undefined);
    await waitFor(() => expect(app).not.toHaveAttribute("aria-disabled", "true"));
    expect(erp).not.toHaveAttribute("aria-disabled", "true");
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
