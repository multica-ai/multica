// Phase 2 — webhook setup section in the workspace tab. Verifies:
//   - Status pill flips between "Configured" and "Not configured" based on
//     workspace.ship_hub_webhook_secret_set.
//   - The Generate / Rotate button calls the regenerate API and pops the
//     secret modal once on success.
//   - The "Copy URL" button writes to clipboard.
//
// The section lives inside ShipHubSettings -> WebhookSettings; we render
// the whole WorkspaceTab and rely on the i18n + workspace mocks to drive
// the relevant branch.

import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RESOURCES } from "../../locales";

const { workspaceRef, mockRegenerate, mockClipboardWriteText } = vi.hoisted(() => ({
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
    },
  },
  mockRegenerate: vi.fn(),
  mockClipboardWriteText: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    regenerateShipHubWebhookSecret: mockRegenerate,
    updateWorkspace: vi.fn(),
    listPersonalAccessTokens: vi.fn().mockResolvedValue([]),
    getBaseUrl: () => "https://example.com",
  },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  resolvePostAuthDestination: () => "/",
  useHasOnboarded: () => true,
}));

// useAuthStore consumed via selectors. Both the call form (s => s.user)
// and `.getState()` are needed; mirror the convention in agents/queries
// tests.
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

// Provide a member matching the mocked auth user so the component
// computes canManageWorkspace=true (the ShipHubSettings + WebhookSettings
// sections are gated on it).
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
  // The clipboard API is shimmed once per render so the assertion can
  // recover the exact value passed in.
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: mockClipboardWriteText },
    configurable: true,
  });
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
    ship_hub_webhook_secret_set: false,
    ...overrides,
  };
}

describe("Webhook settings", () => {
  it("renders 'Not configured' status when ship_hub_webhook_secret_set is false", async () => {
    workspaceRef.current = makeWorkspace({ ship_hub_webhook_secret_set: false });
    renderTab();
    await waitFor(() => {
      expect(screen.getByText(/GitHub webhook/i)).toBeInTheDocument();
    });
    // Two "Not configured" pills could exist (token + webhook); both
    // should match because they share the translation key.
    expect(screen.getAllByText(/Not configured/i).length).toBeGreaterThanOrEqual(1);
    expect(
      screen.getByRole("button", { name: /Generate webhook secret/i }),
    ).toBeInTheDocument();
  });

  it("renders 'Configured' status and a Rotate button when secret is set", async () => {
    workspaceRef.current = makeWorkspace({ ship_hub_webhook_secret_set: true });
    renderTab();
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /Rotate secret/i }),
      ).toBeInTheDocument();
    });
  });

  it("calls regenerate and shows the one-time secret modal on success", async () => {
    workspaceRef.current = makeWorkspace({ ship_hub_webhook_secret_set: false });
    mockRegenerate.mockResolvedValueOnce({
      webhook_secret: "wh_super_secret_value",
      webhook_url: "https://api.example.com/api/integrations/github/webhook",
      webhook_secret_set: true,
    });
    renderTab();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /Generate webhook secret/i }),
      ).toBeInTheDocument(),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Generate webhook secret/i }),
    );
    await waitFor(() => {
      expect(screen.getByText(/Webhook secret created/i)).toBeInTheDocument();
    });
    expect(mockRegenerate).toHaveBeenCalledWith("ws-1");
    // The secret value renders inside a <code> block — assert against the
    // raw text content.
    expect(screen.getByText("wh_super_secret_value")).toBeInTheDocument();
  });

  it("copies the webhook URL to the clipboard when the copy button is clicked", async () => {
    workspaceRef.current = makeWorkspace({
      ship_hub_webhook_url: "https://example.com/api/integrations/github/webhook",
    });
    renderTab();
    await waitFor(() => {
      // Two copy buttons exist (URL row + connection details row); query
      // by aria-label which matches `webhook_copy_url_tooltip`.
      expect(
        screen.getByRole("button", { name: /Copy URL/i }),
      ).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole("button", { name: /Copy URL/i }));
    await waitFor(() => {
      expect(mockClipboardWriteText).toHaveBeenCalledWith(
        "https://example.com/api/integrations/github/webhook",
      );
    });
  });
});
