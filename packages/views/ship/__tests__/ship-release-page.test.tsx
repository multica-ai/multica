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
});
