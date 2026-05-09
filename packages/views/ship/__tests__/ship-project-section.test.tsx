import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";

// Verify the per-project section translates ApiError statuses into the
// error banners the spec calls for: 401 → "GitHub token expired",
// 429 → "Syncing paused", anything else → generic "Sync failed".

const { mockSyncMutateAsync, mockApiError } = vi.hoisted(() => ({
  mockSyncMutateAsync: vi.fn(),
  // Captured here so each test can rethrow whatever status it cares about.
  // We also re-export the actual ApiError class so `instanceof ApiError`
  // (used inside the page) still hits.
  mockApiError: { current: null as Error | null },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: () => [] }),
}));

vi.mock("@multica/core/ship", () => ({
  useProjectPullRequests: () => ({
    data: { pull_requests: [], total: 0 },
    isLoading: false,
    error: null,
    isFetching: false,
  }),
  useDeployEnvironments: () => ({ data: { environments: [] }, isLoading: false }),
  useRecentDeploys: () => ({ data: { deploys: [], total: 0 } }),
  useSyncProject: () => ({
    mutateAsync: mockSyncMutateAsync,
    isPending: false,
  }),
  useUpsertDeployEnvironment: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useLogDeploy: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: () => ({ data: [], isLoading: false }),
    useMutation: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  };
});

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

// We need the real ApiError class so the project-section's instanceof
// check matches what tests throw; everything else from the api module is
// stubbed.
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return { ...actual };
});

import { ApiError } from "@multica/core/api";
import { ShipProjectSection } from "../components/ship-project-section";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderSection() {
  return render(
    <ShipProjectSection
      project={{
        id: "p-1",
        title: "Multica Web",
        icon: null,
        open_pr_count: 3,
        env_count: 2,
      }}
    />,
    { wrapper: I18nWrapper },
  );
}

describe("ShipProjectSection error banners", () => {
  beforeEach(() => {
    mockSyncMutateAsync.mockReset();
    mockApiError.current = null;
  });

  it("renders the project title and Sync now button", () => {
    renderSection();
    expect(screen.getByText("Multica Web")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Sync now/i })).toBeInTheDocument();
  });

  it("shows the 401 token-expired banner when sync rejects with status 401", async () => {
    mockSyncMutateAsync.mockRejectedValueOnce(
      new ApiError("github: invalid or revoked token", 401, "Unauthorized", {
        error: "github: invalid or revoked token",
      }),
    );
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: /Sync now/i }));
    await waitFor(() => {
      expect(screen.getByText(/GitHub token expired/i)).toBeInTheDocument();
    });
  });

  it("shows the 429 rate-limit banner when sync rejects with status 429", async () => {
    mockSyncMutateAsync.mockRejectedValueOnce(
      new ApiError(
        "github: rate limit hit, retry shortly",
        429,
        "Too Many Requests",
        { error: "github: rate limit hit" },
      ),
    );
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: /Sync now/i }));
    await waitFor(() => {
      expect(screen.getByText(/Syncing paused/i)).toBeInTheDocument();
    });
  });
});
