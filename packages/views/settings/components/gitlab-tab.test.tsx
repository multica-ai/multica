import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabTab } from "./gitlab-tab";

const webhookSecret = "glwh_server-gitlab-webhook-secret";

vi.mock("@multica/core/gitlab", () => ({
  gitlabConnectionsOptions: () => ({
    queryKey: ["gitlab", "ws1", "connections"],
    queryFn: async () => ({
      connections: [
        {
          id: "c1",
          workspace_id: "ws1",
          namespace: "my-group",
          namespace_type: "group",
          avatar_url: null,
          created_at: "2026-01-01T00:00:00Z",
          webhook_secret: webhookSecret,
        },
      ],
      configured: true,
      can_manage: true,
    }),
    enabled: true,
  }),
  useDeleteGitLabConnection: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useRotateGitLabWebhookSecret: () => ({ mutateAsync: vi.fn(), isPending: false }),
  deriveGitLabSettings: () => ({
    enabled: true,
    mrSidebar: true,
    issueSync: true,
    commentSync: true,
    issueSyncLabel: "agent",
  }),
  DEFAULT_GITLAB_ISSUE_SYNC_LABEL: "agent",
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (fn: (s: { user: { id: string } }) => unknown) =>
    fn({ user: { id: "u1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "u1", role: "owner" }],
  }),
  workspaceKeys: { list: () => ["workspaces"] },
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws1", name: "Test", settings: {} }),
}));
vi.mock("@multica/core/api", () => ({ api: { updateWorkspace: vi.fn() } }));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (fn: (keys: Record<string, unknown>) => unknown) => {
      const proxy = new Proxy(
        {},
        {
          get: (_t, prop) => {
            if (typeof prop === "string") {
              return new Proxy(
                {},
                {
                  get: (_t2, prop2) => (typeof prop2 === "string" ? prop2 : ""),
                },
              );
            }
            return "";
          },
        },
      );
      const result = fn(proxy as never);
      return typeof result === "string" ? result : String(result);
    },
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("GitLabTab", () => {
  it("shows the per-connection webhook secret for managers", async () => {
    render(<GitLabTab />, { wrapper });
    expect(await screen.findByDisplayValue(webhookSecret)).toBeTruthy();
  });

  it("shows the configurable issue sync label input", async () => {
    render(<GitLabTab />, { wrapper });
    // Placeholder matches DEFAULT_GITLAB_ISSUE_SYNC_LABEL; value is the derived setting.
    const input = await screen.findByPlaceholderText("agent");
    expect((input as HTMLInputElement).value).toBe("agent");
  });
});
