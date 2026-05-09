import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";

// ---------------------------------------------------------------------------
// Hoisted mock state. The mocked stores read from these refs so individual
// tests can mutate them between renders.
// ---------------------------------------------------------------------------

const { workspaceRef, shipDataRef } = vi.hoisted(() => ({
  workspaceRef: {
    current: null as null | {
      id: string;
      ship_hub_enabled: boolean;
      github_token_set: boolean;
    },
  },
  shipDataRef: {
    current: { projects: [] as Array<{ id: string; title: string; icon: string | null; open_pr_count: number; env_count: number }> },
  },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
  useWorkspacePaths: () => ({
    projects: () => "/test/projects",
    settings: () => "/test/settings",
    projectDetail: (id: string) => `/test/projects/${id}`,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/ship", () => ({
  // Match the ApiResponse-y shape useShipProjects returns when consumed
  // — just enough for the page logic.
  useShipProjects: () => ({
    data: shipDataRef.current,
    isLoading: false,
  }),
  useProjectPullRequests: () => ({
    data: { pull_requests: [], total: 0 },
    isLoading: false,
    error: null,
    isFetching: false,
  }),
  useDeployEnvironments: () => ({ data: { environments: [] }, isLoading: false }),
  useRecentDeploys: () => ({ data: { deploys: [], total: 0 } }),
  useSyncProject: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpsertDeployEnvironment: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useLogDeploy: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: () => [] }),
}));

// Stub TanStack Query so child components that call useQuery work without
// a real client. The page itself doesn't call useQuery directly any more
// — it uses the mocked useShipProjects above — but the project section
// reaches for the project list cache via useQuery.
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: () => ({ data: [], isLoading: false, error: null, isFetching: false }),
    useMutation: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  };
});

// AppLink: a plain anchor in tests so we never reach into a routing adapter.
vi.mock("../../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

// PageHeader doesn't need its sidebar trigger logic in tests.
vi.mock("../../layout/page-header", () => ({
  PageHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { ShipPage } from "../components/ship-page";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderPage() {
  return render(<ShipPage />, { wrapper: I18nWrapper });
}

describe("ShipPage", () => {
  beforeEach(() => {
    workspaceRef.current = null;
    shipDataRef.current = { projects: [] };
  });

  it("renders the disabled state when ship_hub_enabled is false", () => {
    workspaceRef.current = {
      id: "ws-1",
      ship_hub_enabled: false,
      github_token_set: false,
    };
    renderPage();
    // The disabled banner uses `page.disabled_title`; rendering its EN
    // copy verifies both the gating logic AND the i18n wiring.
    expect(screen.getByText(/Ship Hub is off/i)).toBeInTheDocument();
  });

  it("renders the no-token state when ship_hub_enabled but token not set", () => {
    workspaceRef.current = {
      id: "ws-1",
      ship_hub_enabled: true,
      github_token_set: false,
    };
    renderPage();
    expect(
      screen.getByText(/Connect GitHub to start shipping/i),
    ).toBeInTheDocument();
    // CTA points at settings — verified via the link's text rather than
    // the href so the test doesn't depend on the path adapter.
    expect(screen.getByText(/Configure GitHub token/i)).toBeInTheDocument();
  });

  it("renders the no-projects empty state when token configured but list is empty", () => {
    workspaceRef.current = {
      id: "ws-1",
      ship_hub_enabled: true,
      github_token_set: true,
    };
    shipDataRef.current = { projects: [] };
    renderPage();
    expect(
      screen.getByText(/No projects with repos yet/i),
    ).toBeInTheDocument();
  });
});
