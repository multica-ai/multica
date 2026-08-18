import { Suspense, type ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  user: {
    id: "user-1",
    onboarded_at: "2026-01-01T00:00:00Z",
  } as { id: string; onboarded_at: string | null } | null,
  isAuthLoading: false,
  workspace: null as { id: string; slug: string } | null,
  workspaceError: false,
  hasBeenSeen: false,
  replace: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: state.replace }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (
    selector: (auth: {
      user: typeof state.user;
      isLoading: boolean;
    }) => unknown,
  ) => selector({ user: state.user, isLoading: state.isAuthLoading }),
}));

vi.mock("@multica/core/workspace", () => ({
  workspaceBySlugOptions: () => ({
    queryKey: ["workspace-by-slug", "acme"],
    queryFn: async () => {
      if (state.workspaceError) throw new Error("temporary failure");
      return state.workspace;
    },
  }),
}));

vi.mock("@multica/core/paths", () => ({
  WorkspaceSlugProvider: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  paths: {
    login: () => "/login",
    onboarding: () => "/onboarding",
  },
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/views/workspace/no-access-page", () => ({
  NoAccessPage: () => <div data-testid="no-access" />,
}));

vi.mock("@multica/views/workspace/welcome-after-onboarding", () => ({
  WelcomeAfterOnboarding: () => null,
}));

vi.mock("@multica/views/workspace/use-workspace-seen", () => ({
  useWorkspaceSeen: () => state.hasBeenSeen,
}));

vi.mock("@multica/ui/components/common/multica-icon", () => ({
  MulticaIcon: () => <div data-testid="workspace-loading" />,
}));

import WorkspaceLayout from "./layout";

function renderLayout() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const resolvedParams = Object.assign(
    Promise.resolve({ workspaceSlug: "acme" }),
    {
      status: "fulfilled",
      value: { workspaceSlug: "acme" },
    },
  );
  const result = render(
    <Suspense fallback={<div data-testid="route-loading" />}>
      <WorkspaceLayout
        params={resolvedParams}
      >
        <div data-testid="workspace-content" />
      </WorkspaceLayout>
    </Suspense>,
    { wrapper },
  );
  return { ...result, queryClient };
}

beforeEach(() => {
  vi.clearAllMocks();
  state.user = {
    id: "user-1",
    onboarded_at: "2026-01-01T00:00:00Z",
  };
  state.isAuthLoading = false;
  state.workspace = null;
  state.workspaceError = false;
  state.hasBeenSeen = false;
});

describe("WorkspaceLayout", () => {
  it("keeps loading instead of showing NoAccess when the initial list request fails", async () => {
    state.workspaceError = true;
    const { queryClient } = renderLayout();

    await waitFor(() => {
      expect(
        queryClient.getQueryState(["workspace-by-slug", "acme"])?.status,
      ).toBe("error");
    });
    expect(screen.queryByTestId("no-access")).toBeNull();
    expect(screen.getByTestId("workspace-loading")).toBeInTheDocument();
  });

  it("shows NoAccess only after an authoritative list omits the slug", async () => {
    renderLayout();

    expect(await screen.findByTestId("no-access")).toBeInTheDocument();
    expect(screen.queryByTestId("workspace-loading")).toBeNull();
  });
});
