// Phase 7a — Active releases rail tests.
//
// Verifies the rail's two states: empty (returns null so the page
// chrome doesn't show an awkward empty card) and populated (renders
// one clickable card per release).

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { ListReleasesResponse } from "@multica/core/types";
import { ShipActiveReleasesRail } from "../components/ship-active-releases-rail";

let activeReleasesFixture: ListReleasesResponse | undefined;

vi.mock("@multica/core/ship", () => ({
  useActiveReleases: () => ({
    data: activeReleasesFixture,
    isLoading: false,
  }),
  useCollapsedProjects: <T,>(
    selector: (state: {
      activeReleasesCollapsed: boolean;
      toggleActiveReleases: () => void;
    }) => T,
  ): T =>
    selector({
      activeReleasesCollapsed: false,
      toggleActiveReleases: () => {},
    }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme" }),
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
    stage: "assembling",
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
    done_at: null,
    rollback_reason: null,
    pr_count: 1,
    merge_paused: false,
    merge_method: "merge",
    ...overrides,
  };
}

describe("ShipActiveReleasesRail", () => {
  it("renders nothing when there are no active releases", () => {
    activeReleasesFixture = { releases: [] };
    const { container } = render(<ShipActiveReleasesRail />, {
      wrapper: Wrapper,
    });
    expect(container.firstChild).toBeNull();
  });

  it("renders one card per release as a clickable workspace-scoped link", () => {
    activeReleasesFixture = {
      releases: [makeRelease({ id: "rel-1", title: "May rollout", pr_count: 3 })],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    expect(screen.getByText("May rollout")).toBeInTheDocument();
    expect(screen.getByText(/3 PRs/)).toBeInTheDocument();
    const link = screen.getByTestId("ship-active-release-view");
    expect(link).toHaveAttribute("href", "/acme/ship/release/rel-1");
  });

  it("applies a stage color class to the stage badge", () => {
    activeReleasesFixture = {
      releases: [
        makeRelease({
          id: "rel-1",
          title: "Prod release",
          stage: "in_production",
        }),
      ],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    const card = screen.getByTestId("ship-active-release-card");
    expect(card).toHaveTextContent("In production");
  });

  it("shows promoted_at as the deployment timestamp when set", () => {
    activeReleasesFixture = {
      releases: [
        makeRelease({
          id: "rel-1",
          title: "Prod release",
          staged_at: "2026-05-09T10:00:00Z",
          promoted_at: "2026-05-09T14:30:00Z",
        }),
      ],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    const timeEl = document.querySelector("time");
    expect(timeEl).not.toBeNull();
    expect(timeEl?.getAttribute("dateTime")).toBe("2026-05-09T14:30:00Z");
  });

  it("falls back to staged_at when promoted_at is absent", () => {
    activeReleasesFixture = {
      releases: [
        makeRelease({
          id: "rel-1",
          title: "Staging release",
          staged_at: "2026-05-09T10:00:00Z",
          promoted_at: null,
        }),
      ],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    const timeEl = document.querySelector("time");
    expect(timeEl?.getAttribute("dateTime")).toBe("2026-05-09T10:00:00Z");
  });

  it("sorts cards by deployment time, newest first", () => {
    activeReleasesFixture = {
      releases: [
        makeRelease({
          id: "rel-old",
          title: "Old release",
          promoted_at: "2026-05-01T08:00:00Z",
        }),
        makeRelease({
          id: "rel-new",
          title: "New release",
          promoted_at: "2026-05-10T16:00:00Z",
        }),
      ],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    const cards = screen.getAllByTestId("ship-active-release-card");
    expect(cards[0]).toHaveTextContent("New release");
    expect(cards[1]).toHaveTextContent("Old release");
  });
});
