// Phase 7d follow-up — DeployWorkflowSettings owner-only section in
// the workspace settings tab. Verifies:
//   - The workflow filename round-trips through PATCH only when the
//     user actually typed something (saving an empty input is a no-op).
//   - The on/off pill correctly reflects ship_hub_deploy_workflow_*_set
//     from the workspace response (drift safety: the field is optional
//     so an older server returning the row without the column should
//     render "off", not crash).
//
// Mocks mirror approval-settings.test.tsx — render the whole
// WorkspaceTab and rely on the i18n + workspace mocks to drive the
// new branch.

import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RESOURCES } from "../../locales";

const { workspaceRef, mockUpdate } = vi.hoisted(() => ({
  workspaceRef: {
    current: null as null | {
      id: string;
      name: string;
      slug: string;
      description: string | null;
      context: string | null;
      orchestrator_agent_id: string | null;
      channels_enabled: boolean;
      channel_retention_days: number | null;
      ship_hub_enabled: boolean;
      github_token_set: boolean;
      ship_hub_webhook_url: string;
      ship_hub_webhook_secret_set: boolean;
      ship_hub_smoke_workflow_set: boolean;
      ship_hub_deploy_workflow_staging_set?: boolean;
      ship_hub_deploy_workflow_production_set?: boolean;
    },
  },
  mockUpdate: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    regenerateShipHubWebhookSecret: vi.fn(),
    updateWorkspace: mockUpdate,
    listPersonalAccessTokens: vi.fn().mockResolvedValue([]),
    getBaseUrl: () => "https://example.com",
  },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  resolvePostAuthDestination: () => "/",
  useHasOnboarded: () => true,
}));

vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "u-1", email: "u@example.com" } };
  const selector = (s: typeof state) => s;
  const useAuthStore = Object.assign(
    (sel?: typeof selector) => (sel ? sel(state) : state),
    { getState: () => state },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/workspace/mutations", () => ({
  useLeaveWorkspace: () => ({ mutateAsync: vi.fn() }),
  useDeleteWorkspace: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: () => [] }),
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: () => [
      {
        id: "m-1",
        workspace_id: "ws-1",
        user_id: "u-1",
        role: "owner",
        name: "u",
        email: "u@example.com",
        avatar_url: null,
        created_at: "2026-01-01T00:00:00Z",
      },
    ],
  }),
  workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: () => [] }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/platform", () => ({ setCurrentWorkspace: vi.fn() }));

vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { WorkspaceTab } from "./workspace-tab";

function renderTab() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        <WorkspaceTab />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function makeWorkspace(
  overrides: Partial<NonNullable<typeof workspaceRef.current>> = {},
) {
  return {
    id: "ws-1",
    name: "Acme",
    slug: "acme",
    description: null,
    context: null,
    orchestrator_agent_id: null,
    channels_enabled: false,
    channel_retention_days: null,
    ship_hub_enabled: true,
    github_token_set: true,
    ship_hub_webhook_url: "https://api.example.com/api/integrations/github/webhook",
    ship_hub_webhook_secret_set: true,
    ship_hub_smoke_workflow_set: false,
    ...overrides,
  };
}

describe("DeployWorkflowSettings", () => {
  it("renders both inputs with off pills when neither workflow is configured", async () => {
    workspaceRef.current = makeWorkspace({
      ship_hub_deploy_workflow_staging_set: false,
      ship_hub_deploy_workflow_production_set: false,
    });
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("deploy-workflow-staging-input")).toBeInTheDocument();
    });
    expect(screen.getByTestId("deploy-workflow-production-input")).toBeInTheDocument();
    // The status pills should both read "off". We render the i18n
    // string directly so the test stays robust against pill styling.
    const stagingPill = screen.getByTestId("deploy-workflow-staging-pill");
    const productionPill = screen.getByTestId("deploy-workflow-production-pill");
    expect(stagingPill).toHaveTextContent(/Auto-detect: off/i);
    expect(productionPill).toHaveTextContent(/Auto-detect: off/i);
    // Save button is disabled until the user types something.
    expect(screen.getByTestId("deploy-workflow-save-button")).toBeDisabled();
  });

  it("renders on pill when staging workflow is already configured server-side", async () => {
    // Drift safety: the field is optional in the type, so a workspace
    // row from before the migration shouldn't crash. With the field
    // explicitly true, the pill flips to "on".
    workspaceRef.current = makeWorkspace({
      ship_hub_deploy_workflow_staging_set: true,
      ship_hub_deploy_workflow_production_set: false,
    });
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("deploy-workflow-staging-pill")).toHaveTextContent(
        /Auto-detect: on/i,
      );
    });
    expect(screen.getByTestId("deploy-workflow-production-pill")).toHaveTextContent(
      /Auto-detect: off/i,
    );
    // The Clear button only appears when the field is set.
    expect(screen.getByTestId("deploy-workflow-staging-clear")).toBeInTheDocument();
    expect(screen.queryByTestId("deploy-workflow-production-clear")).toBeNull();
  });

  it("typing a filename and clicking Save PATCHes only the changed field with paired-bool", async () => {
    workspaceRef.current = makeWorkspace({
      ship_hub_deploy_workflow_staging_set: false,
      ship_hub_deploy_workflow_production_set: false,
    });
    mockUpdate.mockResolvedValueOnce({
      ...workspaceRef.current!,
      ship_hub_deploy_workflow_production_set: true,
    });
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("deploy-workflow-production-input")).toBeInTheDocument();
    });
    const input = screen.getByTestId("deploy-workflow-production-input") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "production.yml" } });
    expect(input.value).toBe("production.yml");

    const save = screen.getByTestId("deploy-workflow-save-button");
    expect(save).not.toBeDisabled();
    fireEvent.click(save);

    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    const [, payload] = mockUpdate.mock.calls[0]!;
    // Only the production field should appear; staging is omitted
    // because the user didn't type anything there.
    expect(payload).toEqual({
      ship_hub_deploy_workflow_production: "production.yml",
      ship_hub_deploy_workflow_production_set: true,
    });
  });
});
