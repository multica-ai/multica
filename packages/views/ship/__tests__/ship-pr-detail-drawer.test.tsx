// PR detail drawer tests — verify the drawer:
//  1. Renders sections from the bundled details payload.
//  2. Hides optional sections when their data is missing.
//  3. Closes on Escape (Sheet primitive default behavior).
//  4. Surfaces a graceful "no description / reviews / actions" copy
//     for empty arrays.
//
// We mock @multica/core/ship at the module boundary so the drawer
// reads from a fixed bundled payload instead of a real query.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { PullRequestDetailsResponse } from "@multica/core/types";
import { ShipPrDetailDrawer } from "../components/ship-pr-detail-drawer";

const detailsRef = vi.hoisted(() => ({
  current: null as PullRequestDetailsResponse | null,
}));
const isLoadingRef = vi.hoisted(() => ({ current: false }));
const isErrorRef = vi.hoisted(() => ({ current: false }));
const openIdRef = vi.hoisted(() => ({ current: null as string | null }));
const closeSpy = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/ship", () => ({
  usePullRequestDetails: () => ({
    data: detailsRef.current,
    isLoading: isLoadingRef.current,
    isError: isErrorRef.current,
  }),
  useShipPrDetailOpenId: () => openIdRef.current,
  useShipPrDetailStore: Object.assign(
    (selector: (state: { close: () => void }) => unknown) =>
      selector({ close: closeSpy }),
    {},
  ),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme", id: "ws-1" }),
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// AppLink mock so we don't need a NavigationProvider in the test
// fixture. Renders a plain anchor; tests assert href/text directly.
vi.mock("../../navigation", async () => {
  const React = await import("react");
  return {
    AppLink: ({
      href,
      children,
      ...props
    }: { href: string; children: React.ReactNode; [k: string]: unknown }) =>
      React.createElement("a", { href, ...props }, children),
  };
});

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

function makeDetails(
  overrides: Partial<PullRequestDetailsResponse> = {},
): PullRequestDetailsResponse {
  return {
    pull_request: {
      id: "pr-1",
      workspace_id: "ws-1",
      project_id: "p-1",
      repo_url: "https://github.com/acme/app",
      number: 42,
      title: "feat: add memory KB",
      state: "open",
      is_draft: false,
      author_login: "alice",
      author_avatar_url: null,
      base_ref: "main",
      head_ref: "feat/memory",
      head_sha: "deadbeef0123456789",
      html_url: "https://github.com/acme/app/pull/42",
      body: "This adds the memory KB feature.\n\n- step 1\n- step 2",
      ci_status: "success",
      review_decision: "",
      mergeable: "MERGEABLE",
      additions: 200,
      deletions: 10,
      changed_files: 8,
      labels: [],
      pr_created_at: "2026-05-01T00:00:00Z",
      pr_updated_at: "2026-05-08T10:00:00Z",
      pr_merged_at: null,
      pr_closed_at: null,
      fetched_at: "2026-05-09T00:00:00Z",
    },
    reviews: [],
    checks: [],
    recent_actions: [],
    stack_children: [],
    ...overrides,
  };
}

beforeEach(() => {
  detailsRef.current = null;
  isLoadingRef.current = false;
  isErrorRef.current = false;
  openIdRef.current = null;
  closeSpy.mockReset();
});

describe("ShipPrDetailDrawer", () => {
  it("renders nothing in DOM when no PR is open", () => {
    openIdRef.current = null;
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    // Sheet portals its content; when closed, the content shouldn't
    // mount the drawer body. The test asserts on a stable testid the
    // body uses.
    expect(screen.queryByTestId("ship-pr-detail-drawer")).not.toBeInTheDocument();
  });

  it("renders the PR title and View on GitHub link when open", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails();
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    expect(screen.getByText("feat: add memory KB")).toBeInTheDocument();
    expect(screen.getByText("#42")).toBeInTheDocument();
    const viewBtn = screen.getByTestId("ship-pr-detail-view-on-github");
    expect(viewBtn).toHaveAttribute(
      "href",
      "https://github.com/acme/app/pull/42",
    );
    expect(viewBtn).toHaveAttribute("target", "_blank");
  });

  it("hides the linked-section chips when no Multica linkage is present", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails(); // no linked_issue/agent_task/channel/release
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    expect(screen.queryByTestId("drawer-linked-issue")).not.toBeInTheDocument();
    expect(screen.queryByTestId("drawer-linked-agent")).not.toBeInTheDocument();
    expect(screen.queryByTestId("drawer-linked-channel")).not.toBeInTheDocument();
    expect(screen.queryByTestId("drawer-linked-release")).not.toBeInTheDocument();
  });

  it("renders the linked issue chip when the PR has an originating issue", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails({
      linked_issue: {
        id: "issue-1",
        workspace_id: "ws-1",
        number: 7,
        identifier: "MUL-7",
        title: "Add memory KB",
        status: "in_progress",
        priority: "medium",
      },
    });
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    const chip = screen.getByTestId("drawer-linked-issue");
    expect(chip).toBeInTheDocument();
    expect(chip).toHaveAttribute("href", "/acme/issues/MUL-7");
  });

  it("shows empty-state copy for missing reviews / actions / checks", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails();
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    expect(screen.getByTestId("drawer-no-reviews")).toBeInTheDocument();
    expect(screen.getByTestId("drawer-no-recent-actions")).toBeInTheDocument();
    expect(screen.getByTestId("drawer-no-checks")).toBeInTheDocument();
  });

  it("renders the description body when populated", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails();
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    const desc = screen.getByTestId("drawer-description");
    expect(desc.textContent).toContain("memory KB feature");
  });

  it("renders reviews + checks when populated", () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails({
      reviews: [
        {
          id: "rev-1",
          reviewer_login: "bob",
          reviewer_avatar_url: null,
          state: "APPROVED",
          body: "lgtm",
          submitted_at: "2026-05-08T10:00:00Z",
        },
      ],
      checks: [
        {
          id: "chk-1",
          name: "unit-tests",
          conclusion: "success",
          status: "completed",
          details_url: "https://github.com/acme/app/runs/1",
          started_at: "2026-05-08T09:00:00Z",
          completed_at: "2026-05-08T09:30:00Z",
        },
      ],
    });
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    expect(screen.getByText("bob")).toBeInTheDocument();
    expect(screen.getByText("lgtm")).toBeInTheDocument();
    expect(screen.getByText("unit-tests")).toBeInTheDocument();
    expect(screen.getByText(/Approved/)).toBeInTheDocument();
  });

  it("calls close when Escape is pressed", async () => {
    openIdRef.current = "pr-1";
    detailsRef.current = makeDetails();
    render(<ShipPrDetailDrawer />, { wrapper: Wrapper });
    expect(screen.getByTestId("ship-pr-detail-drawer")).toBeInTheDocument();
    await act(async () => {
      // Sheet listens for Escape on the document. Dispatch directly.
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(closeSpy).toHaveBeenCalled();
  });
});
