import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { ListReleasesResponse } from "@multica/core/types";
import { ShipReleaseHistory } from "../components/ship-release-history";

let releasesByProject: Record<string, ListReleasesResponse["releases"]>;
let isLoading = false;

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQueries: ({
      queries,
    }: {
      queries: Array<{ projectId?: string; queryKey?: readonly unknown[] }>;
    }) =>
      queries.map((q) => {
        const projectId =
          q.projectId ??
          (Array.isArray(q.queryKey) ? String(q.queryKey.at(-2)) : "");
        return {
          data: { releases: releasesByProject[projectId] ?? [] },
          isLoading,
        };
      }),
  };
});

vi.mock("@multica/core/ship", () => ({
  useShipProjects: () => ({
    data: {
      projects: [
        { id: "p-1", title: "Project One" },
        { id: "p-2", title: "Project Two" },
      ],
    },
    isLoading: false,
  }),
  useCollapsedProjects: <T,>(
    selector: (state: {
      releaseHistoryCollapsed: boolean;
      toggleReleaseHistory: () => void;
    }) => T,
  ): T =>
    selector({
      releaseHistoryCollapsed: false,
      toggleReleaseHistory: () => {},
    }),
  projectReleasesOptions: (
    _wsId: string,
    projectId: string,
    status: "active" | "all",
  ) => ({
    projectId,
    queryKey: ["ship", "ws-1", "releases", "by_project", projectId, status],
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme" }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...rest
  }: {
    href: string;
    children: ReactNode;
  } & Record<string, unknown>) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

function Wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

function makeRelease(
  overrides: Partial<ListReleasesResponse["releases"][number]> & {
    id: string;
    title: string;
  },
): ListReleasesResponse["releases"][number] {
  return {
    workspace_id: "ws-1",
    project_id: "p-1",
    description: null,
    stage: "done",
    risk_level: "medium",
    channel_id: null,
    issue_id: null,
    approver_id: null,
    second_approver_id: null,
    staging_deploy_id: null,
    production_deploy_id: null,
    created_by: null,
    created_at: "2026-05-01T10:00:00Z",
    updated_at: "2026-05-01T10:00:00Z",
    merged_at: null,
    staged_at: null,
    promoted_at: null,
    done_at: "2026-05-01T10:00:00Z",
    rollback_reason: null,
    pr_count: 1,
    merge_paused: false,
    merge_method: "merge",
    ...overrides,
  };
}

describe("ShipReleaseHistory", () => {
  beforeEach(() => {
    releasesByProject = { "p-1": [], "p-2": [] };
    isLoading = false;
  });

  it("renders nothing when all project release lists have no terminal releases", () => {
    releasesByProject["p-1"] = [
      makeRelease({
        id: "rel-active",
        title: "Active",
        stage: "assembling",
        done_at: null,
      }),
    ];
    const { container } = render(<ShipReleaseHistory />, { wrapper: Wrapper });
    expect(container.firstChild).toBeNull();
  });

  it("renders one card per terminal release", () => {
    releasesByProject["p-1"] = [
      makeRelease({ id: "rel-done", title: "Done", stage: "done" }),
      makeRelease({
        id: "rel-cancelled",
        title: "Cancelled",
        stage: "cancelled",
      }),
      makeRelease({
        id: "rel-rolled",
        title: "Rolled back",
        stage: "rolled_back",
      }),
    ];
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    expect(screen.getAllByTestId("ship-history-release-card")).toHaveLength(3);
  });

  it("shows project name on cards when the project is in the lookup map", () => {
    releasesByProject["p-2"] = [
      makeRelease({
        id: "rel-project",
        title: "Project release",
        project_id: "p-2",
      }),
    ];
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    expect(screen.getByText("Project Two")).toBeInTheDocument();
  });

  it("does not show active release stages", () => {
    releasesByProject["p-1"] = [
      makeRelease({
        id: "rel-assembling",
        title: "Assembling",
        stage: "assembling",
      }),
      makeRelease({ id: "rel-merging", title: "Merging", stage: "merging" }),
      makeRelease({
        id: "rel-staging",
        title: "In staging",
        stage: "in_staging",
      }),
      makeRelease({
        id: "rel-promoting",
        title: "Promoting",
        stage: "promoting",
      }),
      makeRelease({
        id: "rel-prod",
        title: "In production",
        stage: "in_production",
      }),
      makeRelease({ id: "rel-done", title: "Done", stage: "done" }),
    ];
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    const cards = screen.getAllByTestId("ship-history-release-card");
    expect(cards).toHaveLength(1);
    expect(cards[0]).toHaveTextContent("Done");
    expect(screen.queryByText("Assembling")).not.toBeInTheDocument();
    expect(screen.queryByText("Merging")).not.toBeInTheDocument();
    expect(screen.queryByText("In staging")).not.toBeInTheDocument();
    expect(screen.queryByText("Promoting")).not.toBeInTheDocument();
    expect(screen.queryByText("In production")).not.toBeInTheDocument();
  });

  it("sorts by most recent terminal date", () => {
    releasesByProject["p-1"] = [
      makeRelease({
        id: "rel-old",
        title: "Old updated",
        stage: "cancelled",
        done_at: null,
        promoted_at: null,
        updated_at: "2026-05-01T10:00:00Z",
      }),
      makeRelease({
        id: "rel-new",
        title: "New done",
        done_at: "2026-05-03T10:00:00Z",
      }),
      makeRelease({
        id: "rel-mid",
        title: "Mid promoted",
        stage: "rolled_back",
        done_at: null,
        promoted_at: "2026-05-02T10:00:00Z",
      }),
    ];
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    const cards = screen.getAllByTestId("ship-history-release-card");
    expect(cards[0]).toHaveTextContent("New done");
    expect(cards[1]).toHaveTextContent("Mid promoted");
    expect(cards[2]).toHaveTextContent("Old updated");
  });

  it("caps output at 10 releases", () => {
    releasesByProject["p-1"] = Array.from({ length: 12 }, (_, i) =>
      makeRelease({
        id: `rel-${i}`,
        title: `Release ${i}`,
        done_at: `2026-05-${String(i + 1).padStart(2, "0")}T10:00:00Z`,
      }),
    );
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    expect(screen.getAllByTestId("ship-history-release-card")).toHaveLength(10);
  });

  it("links each card to the release detail route", () => {
    releasesByProject["p-1"] = [
      makeRelease({ id: "rel-link", title: "Linked release" }),
    ];
    render(<ShipReleaseHistory />, { wrapper: Wrapper });
    expect(screen.getByTestId("ship-history-release-view")).toHaveAttribute(
      "href",
      "/acme/ship/release/rel-link",
    );
  });
});
