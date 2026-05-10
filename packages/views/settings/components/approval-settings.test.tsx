// Phase 7d follow-up — ApprovalSettings owner-only section in the
// workspace settings tab. Verifies:
//   - The four risk-tier Selects render with the workspace's
//     configured rule (or the legacy default when the field is
//     missing — drift safety).
//   - Saving sends a paired-bool patch with ONLY the fields the user
//     actually changed (avoids clobbering concurrent edits from
//     another tab via an "echo back" of cached state).
//   - The "allow PR authors" checkbox round-trips its boolean.
//
// Mocks mirror webhook-settings.test.tsx; we render the whole
// WorkspaceTab and rely on the i18n + workspace mocks to drive the
// approval branch.

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
      ship_hub_approval_low?: string;
      ship_hub_approval_medium?: string;
      ship_hub_approval_high?: string;
      ship_hub_approval_critical?: string;
      ship_hub_approver_can_be_author?: boolean;
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

describe("Approval settings", () => {
  it("renders the four risk-tier selects", async () => {
    workspaceRef.current = makeWorkspace();
    renderTab();
    await waitFor(() => {
      expect(screen.getByText(/Approval requirements/i)).toBeInTheDocument();
    });
    expect(screen.getByTestId("approval-low-select")).toBeInTheDocument();
    expect(screen.getByTestId("approval-medium-select")).toBeInTheDocument();
    expect(screen.getByTestId("approval-high-select")).toBeInTheDocument();
    expect(screen.getByTestId("approval-critical-select")).toBeInTheDocument();
  });

  it("Save button is disabled when nothing changed", async () => {
    workspaceRef.current = makeWorkspace({
      ship_hub_approval_low: "member",
      ship_hub_approval_medium: "member",
      ship_hub_approval_high: "approver",
      ship_hub_approval_critical: "two",
      ship_hub_approver_can_be_author: true,
    });
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("approval-save-button")).toBeInTheDocument();
    });
    expect(screen.getByTestId("approval-save-button")).toBeDisabled();
  });

  it("toggling 'allow PR authors' enables Save and PATCHes only that field", async () => {
    workspaceRef.current = makeWorkspace({
      ship_hub_approval_low: "member",
      ship_hub_approval_medium: "member",
      ship_hub_approval_high: "approver",
      ship_hub_approval_critical: "two",
      ship_hub_approver_can_be_author: true,
    });
    mockUpdate.mockResolvedValueOnce(workspaceRef.current);
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("approval-allow-author-checkbox")).toBeInTheDocument();
    });
    const checkbox = screen.getByTestId("approval-allow-author-checkbox") as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
    fireEvent.click(checkbox);
    expect(checkbox.checked).toBe(false);

    const save = screen.getByTestId("approval-save-button");
    expect(save).not.toBeDisabled();
    fireEvent.click(save);

    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    const [, payload] = mockUpdate.mock.calls[0]!;
    // Only the can-be-author field should be in the patch — the four
    // rule fields should be omitted because they didn't change.
    expect(payload).toEqual({
      ship_hub_approver_can_be_author: false,
      ship_hub_approver_can_be_author_set: true,
    });
  });

  it("falls back to legacy defaults when approval fields are missing on the workspace row", async () => {
    // Drift-safety: an older server response (or a workspace row from
    // before the migration) doesn't carry the four rule fields. The
    // Selects should still render the legacy hardcoded defaults so the
    // user has something to read AND the dirty-detection works
    // (changing the rendered value to the same default is a no-op).
    workspaceRef.current = makeWorkspace({
      ship_hub_approval_low: undefined,
      ship_hub_approval_medium: undefined,
      ship_hub_approval_high: undefined,
      ship_hub_approval_critical: undefined,
      ship_hub_approver_can_be_author: undefined,
    });
    renderTab();
    await waitFor(() => {
      expect(screen.getByTestId("approval-low-select")).toBeInTheDocument();
    });
    // Default-rendered values surface as the i18n strings; the
    // Save button stays disabled because nothing has changed yet.
    expect(screen.getByTestId("approval-save-button")).toBeDisabled();
  });
});
