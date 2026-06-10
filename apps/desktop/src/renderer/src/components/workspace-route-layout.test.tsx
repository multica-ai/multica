import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

// vi.hoisted shared state for all the stores / hooks the layout consumes.
const state = vi.hoisted(() => ({
  user: null as { id: string } | null,
  isAuthLoading: false,
  overlay: null as { type: string } | null,
  workspace: null as { id: string; slug: string } | null,
  listFetched: true,
  wsList: [] as { id: string; slug: string }[],
  workspaceSeen: true,
  modalRenders: 0,
  modalAriaLabel: "source-backfill-modal-marker",
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) => {
    if (selector.toString().includes("isLoading"))
      return state.isAuthLoading;
    return state.user;
  };
  return { useAuthStore };
});

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/workspace", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/workspace")>(
    "@multica/core/workspace",
  );
  return {
    ...actual,
    workspaceBySlugOptions: () => ({
      queryKey: ["workspace-by-slug"],
      queryFn: async () => state.workspace,
    }),
    workspaceListOptions: () => ({
      queryKey: ["workspace-list"],
      queryFn: async () => state.wsList,
    }),
  };
});

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    paths: {
      ...actual.paths,
      login: () => "/login",
    },
  };
});

vi.mock("@multica/views/workspace/use-workspace-seen", () => ({
  useWorkspaceSeen: () => state.workspaceSeen,
}));

vi.mock("@multica/views/workspace/welcome-after-onboarding", () => ({
  WelcomeAfterOnboarding: () => null,
}));

vi.mock("@multica/views/layout", () => ({
  WorkspacePresencePrefetch: () => null,
}));

// The point of this whole test: assert the desktop layout mounts the
// SourceBackfillModal. We stub the real component with a marker that
// renders only when the layout actually rendered it (and not e.g.
// suppressed by overlayActive).
vi.mock("@multica/views/onboarding", () => ({
  SourceBackfillModal: () => {
    state.modalRenders += 1;
    return <div data-testid={state.modalAriaLabel} />;
  },
}));

vi.mock("@/stores/tab-store", () => ({
  useTabStore: Object.assign(() => null, {
    getState: () => ({ validateWorkspaceSlugs: vi.fn() }),
  }),
}));

vi.mock("@/stores/window-overlay-store", () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useWindowOverlayStore };
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceRouteLayout } from "./workspace-route-layout";

function renderLayout() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  // Seed the workspace queries so the gate inside the layout passes
  // synchronously — the real hook reads from cache.
  qc.setQueryData(["workspace-by-slug"], state.workspace);
  qc.setQueryData(["workspace-list"], state.wsList);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/acme/issues"]}>
        <Routes>
          <Route path=":workspaceSlug/*" element={<WorkspaceRouteLayout />}>
            <Route path="*" element={<div data-testid="outlet" />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.user = { id: "u1" };
  state.isAuthLoading = false;
  state.overlay = null;
  state.workspace = { id: "ws-1", slug: "acme" };
  state.listFetched = true;
  state.wsList = [{ id: "ws-1", slug: "acme" }];
  state.workspaceSeen = true;
  state.modalRenders = 0;
});

describe("WorkspaceRouteLayout", () => {
  it("mounts SourceBackfillModal when no WindowOverlay is active", () => {
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).not.toBeNull();
    expect(state.modalRenders).toBeGreaterThan(0);
  });

  it("suppresses SourceBackfillModal while a WindowOverlay is active", () => {
    state.overlay = { type: "new-workspace" };
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).toBeNull();
    expect(state.modalRenders).toBe(0);
  });
});
