// Phase 7a — Active releases rail tests.
//
// Verifies the rail's two states: empty (returns null so the page
// chrome doesn't show an awkward empty card) and populated (renders
// one card per release with a "View" link).

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

describe("ShipActiveReleasesRail", () => {
  it("renders nothing when there are no active releases", () => {
    activeReleasesFixture = { releases: [] };
    const { container } = render(<ShipActiveReleasesRail />, {
      wrapper: Wrapper,
    });
    expect(container.firstChild).toBeNull();
  });

  it("renders one card per release with a workspace-scoped View link", () => {
    activeReleasesFixture = {
      releases: [
        {
          id: "rel-1",
          workspace_id: "ws-1",
          project_id: "p-1",
          title: "May rollout",
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
          created_at: "",
          updated_at: "",
          merged_at: null,
          staged_at: null,
          promoted_at: null,
          done_at: null,
          rollback_reason: null,
          pr_count: 3,
          merge_paused: false,
          merge_method: "merge",
        },
      ],
    };
    render(<ShipActiveReleasesRail />, { wrapper: Wrapper });
    expect(screen.getByText("May rollout")).toBeInTheDocument();
    expect(screen.getByText(/3 PRs/)).toBeInTheDocument();
    const link = screen.getByTestId("ship-active-release-view");
    expect(link).toHaveAttribute("href", "/acme/ship/release/rel-1");
  });
});
