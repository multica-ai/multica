import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

// vi.hoisted shared state for all the stores / hooks the layout consumes.
const state = vi.hoisted(() => ({
  user: null as { id: string } | null,
  isAuthLoading: false,
  overlay: null as { type: string } | null,
  workspace: null as { id: string; slug: string } | null,
  workspaceError: false,
  listFetched: true,
  wsList: [] as { id: string; slug: string }[],
  workspaceSeen: true,
  modalRenders: 0,
  modalAriaLabel: "source-backfill-modal-marker",
  currentSlug: null as string | null,
  pendingDeletes: new Set<string>(),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) => {
    if (selector.toString().includes("isLoading"))
      return state.isAuthLoading;
    return state.user;
  };
  return { useAuthStore };
});

// A real-enough stand-in for the platform singleton: the layout both writes
// and reads it (the clear paths check `getCurrentSlug()` before wiping), so a
// bare vi.fn() would make every guard trivially true.
vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn((slug: string | null) => {
    state.currentSlug = slug;
  }),
  getCurrentSlug: () => state.currentSlug,
}));

vi.mock("@multica/core/workspace/pending-delete", () => ({
  isWorkspaceDeletePending: (id: string) => state.pendingDeletes.has(id),
}));

vi.mock("@multica/core/workspace", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/workspace")>(
    "@multica/core/workspace",
  );
  return {
    ...actual,
    workspaceBySlugOptions: () => ({
      queryKey: ["workspace-by-slug"],
      queryFn: async () => {
        if (state.workspaceError) throw new Error("temporary failure");
        return state.workspace;
      },
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
  const result = render(
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
  return { ...result, queryClient: qc };
}

beforeEach(() => {
  state.user = { id: "u1" };
  state.isAuthLoading = false;
  state.overlay = null;
  state.workspace = { id: "ws-1", slug: "acme" };
  state.workspaceError = false;
  state.listFetched = true;
  state.wsList = [{ id: "ws-1", slug: "acme" }];
  state.workspaceSeen = true;
  state.modalRenders = 0;
  state.currentSlug = null;
  state.pendingDeletes = new Set<string>();
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

  it("keeps workspace content mounted when a background refetch fails", async () => {
    const { queryByTestId, queryClient } = renderLayout();
    expect(queryByTestId("outlet")).not.toBeNull();

    state.workspaceError = true;
    await act(async () => {
      await queryClient.refetchQueries({ queryKey: ["workspace-by-slug"] });
    });

    await waitFor(() => {
      expect(queryClient.getQueryState(["workspace-by-slug"])?.status).toBe(
        "error",
      );
    });
    expect(queryClient.getQueryData(["workspace-by-slug"])).toEqual({
      id: "ws-1",
      slug: "acme",
    });
    expect(queryByTestId("outlet")).not.toBeNull();
  });
});

/**
 * MUL-6231 / #7021. This layout is the only owner of the platform workspace
 * singleton, and it used to only ever SET it. Deleting the active workspace
 * therefore left the singleton pointing at a workspace that no longer existed,
 * which is what kept the desktop shell mounting workspace-scoped chrome over
 * nothing until a `useWorkspaceId()` throw blanked the whole renderer.
 */
describe("WorkspaceRouteLayout workspace singleton lifecycle", () => {
  it("adopts the workspace while it resolves", () => {
    renderLayout();
    expect(state.currentSlug).toBe("acme");
  });

  it("releases the singleton once the workspace stops resolving", () => {
    state.currentSlug = "acme";
    state.workspace = null;
    state.wsList = [];

    renderLayout();

    expect(state.currentSlug).toBeNull();
  });

  it("does not re-adopt a workspace whose delete this client started", () => {
    // The exact post-delete window: useDeleteWorkspace has cleared the
    // singleton and navigated, but its list invalidation is still in flight so
    // the deleted workspace is very much still in the cache. Re-adopting here
    // would stamp the dead slug back over the delete flow's own cleanup.
    state.currentSlug = null;
    state.pendingDeletes = new Set(["ws-1"]);

    renderLayout();

    expect(state.currentSlug).toBeNull();
  });

  it("releases the singleton on unmount", () => {
    const { unmount } = renderLayout();
    expect(state.currentSlug).toBe("acme");

    unmount();

    expect(state.currentSlug).toBeNull();
  });

  it("leaves the singleton alone when it already points at another workspace", () => {
    // Workspace switch: React renders the incoming layout (which adopts the
    // new slug) before running the outgoing layout's cleanup. An unguarded
    // clear would wipe the workspace context that just arrived.
    const { unmount } = renderLayout();
    state.currentSlug = "other";

    unmount();

    expect(state.currentSlug).toBe("other");
  });
});
