// Phase 7a — ShipReleasePage tests.
//
// Mocks the @multica/core/ship module so the page can render without
// hitting a real backend; verifies stage badge, progress bar,
// PR list, event timeline, and that the cancel button only renders
// in the assembling stage.

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type {
  Release,
  ReleaseDetailResponse,
  ReleaseEvent,
  ReleasePullRequest,
} from "@multica/core/types";
import { ShipReleasePage } from "../components/ship-release-page";

let detailFixture: ReleaseDetailResponse;

// Phase 7b — the merge train mutations are added to the mock so the
// release page renders the new buttons without exploding. Each
// returns a stable object whose mutateAsync the tests can reach
// through the exported handles below.
const startMergeMutateAsync = vi.fn().mockResolvedValue({});
const resumeMergeMutateAsync = vi.fn().mockResolvedValue({});
const abortMergeMutateAsync = vi.fn().mockResolvedValue({});

// Phase 7c — staging-stage mutations. Declared via vi.hoisted so the
// vi.mock factory below (which executes before module-top declarations
// in the runtime due to hoisting) sees these refs.
const {
  runSmokeMutateAsync,
  markSmokePassMutateAsync,
  markVerifiedMutateAsync,
  unverifyMutateAsync,
} = vi.hoisted(() => ({
  runSmokeMutateAsync: vi.fn().mockResolvedValue({}),
  markSmokePassMutateAsync: vi.fn().mockResolvedValue({}),
  markVerifiedMutateAsync: vi.fn().mockResolvedValue({}),
  unverifyMutateAsync: vi.fn().mockResolvedValue({}),
}));

vi.mock("@multica/core/ship", () => ({
  useReleaseDetail: () => ({
    data: detailFixture,
    isLoading: false,
    isError: false,
  }),
  useCancelRelease: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useRemovePullRequestFromRelease: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useStartMergeTrain: () => ({
    mutateAsync: startMergeMutateAsync,
    isPending: false,
  }),
  useResumeMergeTrain: () => ({
    mutateAsync: resumeMergeMutateAsync,
    isPending: false,
  }),
  useAbortMergeTrain: () => ({
    mutateAsync: abortMergeMutateAsync,
    isPending: false,
  }),
  // Phase 7c — staging-stage mutations. Each test that exercises
  // these overrides the spy via the shared module-scope refs below.
  useRunSmokeTestsForRelease: () => ({
    mutateAsync: runSmokeMutateAsync,
    isPending: false,
  }),
  useMarkSmokeManualPass: () => ({
    mutateAsync: markSmokePassMutateAsync,
    isPending: false,
  }),
  useMarkReleaseVerified: () => ({
    mutateAsync: markVerifiedMutateAsync,
    isPending: false,
  }),
  useUnverifyRelease: () => ({
    mutateAsync: unverifyMutateAsync,
    isPending: false,
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

function makeRelease(overrides: Partial<Release> = {}): Release {
  return {
    id: "rel-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    title: "May rollout",
    description: "Memory KB + inbox auto-archive",
    stage: "assembling",
    risk_level: "medium",
    channel_id: null,
    issue_id: null,
    approver_id: null,
    second_approver_id: null,
    staging_deploy_id: null,
    production_deploy_id: null,
    created_by: null,
    created_at: "2026-05-09T00:00:00Z",
    updated_at: "2026-05-09T01:00:00Z",
    merged_at: null,
    staged_at: null,
    promoted_at: null,
    done_at: null,
    rollback_reason: null,
    pr_count: 2,
    merge_paused: false,
    merge_method: "merge",
    ...overrides,
  };
}

function makeReleasePR(overrides: Partial<ReleasePullRequest> = {}): ReleasePullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 100,
    title: "memory: KB UI",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "sha",
    html_url: "https://github.com/acme/app/pull/100",
    body: null,
    ci_status: "success",
    review_decision: "APPROVED",
    mergeable: "MERGEABLE",
    additions: 100,
    deletions: 10,
    changed_files: 5,
    labels: [],
    pr_created_at: "",
    pr_updated_at: "",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "",
    risk_level: "medium",
    position: 0,
    merged_sha: null,
    merged_at_release: null,
    merge_error: null,
    added_at: "",
    is_active: true,
    merge_state: "queued",
    ...overrides,
  };
}

function makeEvent(overrides: Partial<ReleaseEvent> = {}): ReleaseEvent {
  return {
    id: "evt-1",
    release_id: "rel-1",
    event_type: "created",
    actor_user_id: null,
    payload: null,
    created_at: "2026-05-09T00:00:00Z",
    ...overrides,
  };
}

describe("ShipReleasePage", () => {
  it("renders the assembling stage badge + progress bar + cancel button", () => {
    detailFixture = {
      release: makeRelease({ stage: "assembling" }),
      pull_requests: [makeReleasePR()],
      events: [makeEvent()],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });

    expect(screen.getByTestId("release-stage-badge")).toHaveTextContent(
      /assembling/i,
    );
    expect(screen.getByTestId("release-stage-progress")).toBeInTheDocument();
    expect(screen.getByTestId("release-cancel-button")).toBeInTheDocument();
  });

  it("hides the cancel button outside the assembling stage", () => {
    detailFixture = {
      release: makeRelease({ stage: "in_production" }),
      pull_requests: [makeReleasePR()],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    expect(screen.queryByTestId("release-cancel-button")).not.toBeInTheDocument();
  });

  it("renders the terminal banner instead of the progress bar for cancelled releases", () => {
    detailFixture = {
      release: makeRelease({ stage: "cancelled", rollback_reason: "no go" }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    expect(screen.getByTestId("release-terminal-banner")).toBeInTheDocument();
    expect(screen.queryByTestId("release-stage-progress")).not.toBeInTheDocument();
    expect(screen.getByText(/no go/)).toBeInTheDocument();
  });

  it("shows PR list and timeline contents", () => {
    detailFixture = {
      release: makeRelease(),
      pull_requests: [
        makeReleasePR({ id: "pr-a", number: 101, title: "first PR" }),
        makeReleasePR({ id: "pr-b", number: 102, title: "second PR" }),
      ],
      events: [
        makeEvent({ id: "ev-1", event_type: "created" }),
        makeEvent({ id: "ev-2", event_type: "channel_created" }),
      ],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    expect(screen.getByText("first PR")).toBeInTheDocument();
    expect(screen.getByText("second PR")).toBeInTheDocument();
    expect(screen.getByText("Release created")).toBeInTheDocument();
    expect(screen.getByText("Channel created")).toBeInTheDocument();
  });

  // Phase 7b — start_merge button gating.
  it("renders the start merge train button when the release is assembling", () => {
    detailFixture = {
      release: makeRelease({ stage: "assembling", risk_level: "low" }),
      pull_requests: [makeReleasePR()],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const btn = screen.getByTestId("release-start-merge-button");
    expect(btn).toBeInTheDocument();
    expect(btn).not.toBeDisabled();
  });

  it("disables the start merge train button when an approver is missing for medium risk", () => {
    detailFixture = {
      release: makeRelease({
        stage: "assembling",
        risk_level: "medium",
        approver_id: null,
      }),
      pull_requests: [makeReleasePR()],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const btn = screen.getByTestId("release-start-merge-button");
    expect(btn).toBeDisabled();
    expect(screen.getByTestId("release-start-preconditions")).toBeInTheDocument();
  });

  it("renders per-PR merge_state pills for merging release", () => {
    detailFixture = {
      release: makeRelease({ stage: "merging" }),
      pull_requests: [
        makeReleasePR({
          id: "pr-a",
          number: 101,
          title: "first",
          merge_state: "merged",
          merged_sha: "abcdef1234567",
        }),
        makeReleasePR({
          id: "pr-b",
          number: 102,
          title: "second",
          merge_state: "merging",
        }),
        makeReleasePR({
          id: "pr-c",
          number: 103,
          title: "third",
          merge_state: "queued",
        }),
      ],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const pills = screen.getAllByTestId("release-pr-merge-state");
    expect(pills).toHaveLength(3);
    expect(pills[0]).toHaveAttribute("data-state", "merged");
    expect(pills[1]).toHaveAttribute("data-state", "merging");
    expect(pills[2]).toHaveAttribute("data-state", "queued");
    expect(screen.getByTestId("release-merging-progress")).toBeInTheDocument();
    expect(screen.getByTestId("release-abort-button")).toBeInTheDocument();
  });

  it("renders the paused banner and resume controls when merge is paused", () => {
    detailFixture = {
      release: makeRelease({ stage: "merging", merge_paused: true }),
      pull_requests: [
        makeReleasePR({ id: "pr-a", number: 101, merge_state: "merged" }),
        makeReleasePR({
          id: "pr-b",
          number: 102,
          merge_state: "failed",
          merge_error: "branch is not mergeable",
        }),
        makeReleasePR({ id: "pr-c", number: 103, merge_state: "queued" }),
      ],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    expect(screen.getByTestId("release-paused-banner")).toBeInTheDocument();
    expect(screen.getByText(/branch is not mergeable/)).toBeInTheDocument();
    expect(screen.getByTestId("release-resume-button")).toBeInTheDocument();
    expect(screen.getByTestId("release-resume-with-skip-button")).toBeInTheDocument();
    expect(screen.getByTestId("release-abort-button")).toBeInTheDocument();
  });

  it("calls resume mutation when the Resume button is clicked", async () => {
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();
    resumeMergeMutateAsync.mockClear();
    detailFixture = {
      release: makeRelease({ stage: "merging", merge_paused: true }),
      pull_requests: [
        makeReleasePR({
          id: "pr-b",
          number: 102,
          merge_state: "failed",
          merge_error: "conflict",
        }),
      ],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    await user.click(screen.getByTestId("release-resume-button"));
    expect(resumeMergeMutateAsync).toHaveBeenCalledWith({});
  });

  // ---------------------------------------------------------------------
  // Phase 7c — staging-stage rendering + interactions.
  // ---------------------------------------------------------------------

  it("renders the smoke-status pill in_staging based on smoke_status", () => {
    detailFixture = {
      release: makeRelease({
        stage: "in_staging",
        smoke_status: "queued",
        merged_main_sha: "abc1234deadbeef",
        staging_deploy_id: "deploy-1",
        staged_at: "2026-05-09T00:30:00Z",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const panel = screen.getByTestId("release-smoke-panel");
    expect(panel).toHaveAttribute("data-smoke-status", "queued");
  });

  it("disables Run smoke when status is in_progress", () => {
    detailFixture = {
      release: makeRelease({
        stage: "in_staging",
        smoke_status: "in_progress",
        merged_main_sha: "abc1234",
        staging_deploy_id: "deploy-1",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const btn = screen.getByTestId("release-run-smoke-button");
    expect(btn).toBeDisabled();
  });

  it("renders Mark verified button gated by approver risk-tier (medium = enabled)", () => {
    detailFixture = {
      release: makeRelease({
        stage: "in_staging",
        risk_level: "medium",
        smoke_status: "completed_success",
        staging_deploy_id: "deploy-1",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const btn = screen.getByTestId("release-verify-button");
    // Medium risk doesn't enforce approver-equality at the UI level
    // (the server still does); the button is enabled with smoke
    // completed_success.
    expect(btn).not.toBeDisabled();
  });

  it("disables Mark verified for high risk when smoke hasn't completed", () => {
    detailFixture = {
      release: makeRelease({
        stage: "in_staging",
        risk_level: "high",
        smoke_status: "completed_failure",
        staging_deploy_id: "deploy-1",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    const btn = screen.getByTestId("release-verify-button");
    expect(btn).toBeDisabled();
  });

  it("renders verified banner + Unverify button in verifying stage", () => {
    detailFixture = {
      release: makeRelease({
        stage: "verifying",
        qa_verified_at: "2026-05-09T01:00:00Z",
        qa_verified_by: "user-abcdef12",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    expect(screen.getByTestId("release-verified-banner")).toBeInTheDocument();
    expect(screen.getByTestId("release-unverify-button")).toBeInTheDocument();
  });

  it("submits Mark verified dialog with note", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    const user = userEvent.setup();
    markVerifiedMutateAsync.mockClear();
    detailFixture = {
      release: makeRelease({
        stage: "in_staging",
        smoke_status: "completed_success",
        risk_level: "low",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    await user.click(screen.getByTestId("release-verify-button"));
    await user.click(screen.getByTestId("release-verify-submit"));
    expect(markVerifiedMutateAsync).toHaveBeenCalledWith({ note: "" });
  });

  it("Unverify dialog disables submit until reason is typed", async () => {
    const userEvent = (await import("@testing-library/user-event")).default;
    const user = userEvent.setup();
    unverifyMutateAsync.mockClear();
    detailFixture = {
      release: makeRelease({
        stage: "verifying",
        qa_verified_at: "2026-05-09T01:00:00Z",
        qa_verified_by: "user-abcdef12",
      }),
      pull_requests: [],
      events: [],
    };
    render(<ShipReleasePage releaseId="rel-1" />, { wrapper: Wrapper });
    await user.click(screen.getByTestId("release-unverify-button"));
    const submit = screen.getByTestId("release-unverify-submit");
    expect(submit).toBeDisabled();

    const reasonInput = screen.getByTestId("release-unverify-reason");
    await user.type(reasonInput, "found a bug");
    expect(submit).not.toBeDisabled();

    await user.click(submit);
    expect(unverifyMutateAsync).toHaveBeenCalledWith({ reason: "found a bug" });
  });
});
