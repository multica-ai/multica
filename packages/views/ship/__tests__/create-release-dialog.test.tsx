// Phase 7a — CreateReleaseDialog tests.
//
// We exercise the precondition logic the dialog enforces before
// hitting the server: ineligible PRs surface as warnings AND block
// submit; missing approver for high+ risk surfaces as a soft
// warning; happy path calls the create mutation with the right
// payload.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { CreateReleaseResponse, PullRequest } from "@multica/core/types";
import { CreateReleaseDialog } from "../components/create-release-dialog";

const createMutateAsync = vi.hoisted(() =>
  vi.fn<(args: unknown) => Promise<CreateReleaseResponse>>(),
);
const clearSelectionSpy = vi.hoisted(() => vi.fn());

// Mock @multica/core/ship — selection store + create-release mutation.
// The selection store is what the dialog uses to clear selection on
// success, so we intercept it.
vi.mock("@multica/core/ship", () => ({
  useCreateRelease: () => ({
    mutateAsync: createMutateAsync,
    isPending: false,
  }),
  useShipSelection: Object.assign(
    (selector: (state: { clear: () => void }) => unknown) =>
      selector({ clear: clearSelectionSpy }),
    {},
  ),
}));

// Mock workspace + members + paths so the dialog mounts standalone.
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme", id: "ws-1" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({
    queryKey: ["members", wsId],
    queryFn: () => [
      { user_id: "u-alice", name: "Alice", email: "alice@a.com" },
      { user_id: "u-bob", name: "Bob", email: "bob@a.com" },
    ],
  }),
}));

// Mock the navigation adapter — push() is observed but doesn't go anywhere.
const navigationPush = vi.hoisted(() => vi.fn());
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: navigationPush }),
}));

const toastSpies = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}));
vi.mock("sonner", () => ({ toast: toastSpies }));

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

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 1,
    title: "memory: add KB",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "sha",
    html_url: "https://github.com/acme/app/pull/1",
    body: null,
    ci_status: "success",
    review_decision: "APPROVED",
    mergeable: "MERGEABLE",
    additions: 100,
    deletions: 10,
    changed_files: 5,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-08T10:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T00:00:00Z",
    risk_level: "medium",
    ...overrides,
  };
}

beforeEach(() => {
  createMutateAsync.mockReset();
  clearSelectionSpy.mockReset();
  navigationPush.mockReset();
  toastSpies.success.mockReset();
  toastSpies.error.mockReset();
});

describe("CreateReleaseDialog", () => {
  it("renders selected PRs in the checklist", () => {
    render(
      <CreateReleaseDialog
        open
        onOpenChange={() => {}}
        projectId="p-1"
        selectedPullRequests={[
          makePR({ id: "pr-1", number: 100, title: "memory: a" }),
          makePR({ id: "pr-2", number: 101, title: "memory: b" }),
        ]}
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("memory: a")).toBeInTheDocument();
    expect(screen.getByText("memory: b")).toBeInTheDocument();
  });

  it("blocks submit and surfaces a warning when a PR is ineligible (draft)", () => {
    render(
      <CreateReleaseDialog
        open
        onOpenChange={() => {}}
        projectId="p-1"
        selectedPullRequests={[
          makePR({ id: "pr-1", is_draft: true }),
        ]}
      />,
      { wrapper: Wrapper },
    );
    const submit = screen.getByTestId("release-submit");
    expect(submit).toBeDisabled();
    expect(screen.getByTestId("release-warnings")).toBeInTheDocument();
    expect(
      screen.getByText(/PR #1 is not eligible.*is a draft/),
    ).toBeInTheDocument();
  });

  it("surfaces a soft warning when risk requires an approver but none is set", () => {
    render(
      <CreateReleaseDialog
        open
        onOpenChange={() => {}}
        projectId="p-1"
        selectedPullRequests={[makePR({ risk_level: "high" })]}
      />,
      { wrapper: Wrapper },
    );
    expect(screen.getByTestId("release-warnings")).toBeInTheDocument();
    expect(
      screen.getByText(/Risk level high requires an approver/),
    ).toBeInTheDocument();
  });

  it("submits with the expected payload on the happy path", async () => {
    const user = userEvent.setup();
    createMutateAsync.mockResolvedValueOnce({
      release: {
        id: "rel-new",
        workspace_id: "ws-1",
        project_id: "p-1",
        title: "memory release",
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
        pr_count: 1,
        merge_paused: false,
        merge_method: "merge",
      },
      warnings: [],
    });

    render(
      <CreateReleaseDialog
        open
        onOpenChange={() => {}}
        projectId="p-1"
        selectedPullRequests={[makePR({ id: "pr-1", risk_level: "low" })]}
      />,
      { wrapper: Wrapper },
    );
    const submit = screen.getByTestId("release-submit");
    expect(submit).not.toBeDisabled();
    await user.click(submit);
    expect(createMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        pull_request_ids: ["pr-1"],
      }),
    );
    // Selection cleared on success and navigation kicked off.
    expect(clearSelectionSpy).toHaveBeenCalled();
    expect(navigationPush).toHaveBeenCalledWith(
      "/acme/ship/release/rel-new",
    );
  });
});
