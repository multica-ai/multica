import { describe, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabTab } from "./gitlab-tab";

vi.mock("@multica/core/gitlab", () => ({
  gitlabConnectionsOptions: () => ({
    queryKey: ["gitlab", "ws1", "connections"],
    queryFn: async () => ({ connections: [], configured: false, can_manage: false }),
    enabled: true,
  }),
  useDeleteGitLabConnection: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (fn: (s: { user: { id: string } }) => unknown) =>
    fn({ user: { id: "u1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("GitLabTab", () => {
  it("renders without crashing", () => {
    render(<GitLabTab />, { wrapper });
  });
});
