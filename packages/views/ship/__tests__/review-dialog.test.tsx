import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import type { ActionResult, PullRequest } from "@multica/core/types";
import { ReviewDialog } from "../components/review-dialog";

// Mock the mutation hook used by the dialog. We control its return value
// per-test so happy-path / error-path / pending paths can each be
// asserted independently.
const submitMutateAsync = vi.hoisted(() =>
  vi.fn<(args: unknown) => Promise<ActionResult>>(),
);
const submitState = vi.hoisted(() => ({ isPending: false }));

vi.mock("@multica/core/ship", () => ({
  useSubmitPullRequestReview: () => ({
    mutateAsync: submitMutateAsync,
    isPending: submitState.isPending,
  }),
}));

const toastSpies = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: toastSpies,
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

function makePR(overrides: Partial<PullRequest> = {}): PullRequest {
  return {
    id: "pr-1",
    workspace_id: "ws-1",
    project_id: "p-1",
    repo_url: "https://github.com/acme/app",
    number: 42,
    title: "Add feature",
    state: "open",
    is_draft: false,
    author_login: "alice",
    author_avatar_url: null,
    base_ref: "main",
    head_ref: "feat/x",
    head_sha: "deadbee",
    html_url: "https://github.com/acme/app/pull/42",
    body: null,
    ci_status: "success",
    review_decision: "",
    mergeable: "MERGEABLE",
    additions: 1,
    deletions: 0,
    changed_files: 1,
    labels: [],
    pr_created_at: "2026-05-01T00:00:00Z",
    pr_updated_at: "2026-05-09T10:00:00Z",
    pr_merged_at: null,
    pr_closed_at: null,
    fetched_at: "2026-05-09T11:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  submitMutateAsync.mockReset();
  submitState.isPending = false;
  toastSpies.success.mockReset();
  toastSpies.error.mockReset();
  toastSpies.info.mockReset();
});

describe("ReviewDialog", () => {
  it("renders the dialog title and the View diff link pointing at /files", () => {
    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );
    expect(
      screen.getByText(/Review #42 · Add feature/i),
    ).toBeInTheDocument();
    const link = screen.getByTestId("review-dialog-view-diff");
    expect(link).toHaveAttribute(
      "href",
      "https://github.com/acme/app/pull/42/files",
    );
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("Approve is enabled with an empty body; Comment and Request changes are disabled", () => {
    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );
    // Approve: enabled with no body — GitHub allows approval-only.
    expect(screen.getByTestId("review-dialog-submit-approve")).not.toBeDisabled();
    // Comment + Request changes: disabled until a body is present.
    expect(screen.getByTestId("review-dialog-submit-comment")).toBeDisabled();
    expect(
      screen.getByTestId("review-dialog-submit-request-changes"),
    ).toBeDisabled();
  });

  it("Comment and Request changes enable once the textarea has content", async () => {
    const user = userEvent.setup();
    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );
    const textarea = screen.getByLabelText(/Comment$/i);
    await user.type(textarea, "looks good");
    await waitFor(() => {
      expect(screen.getByTestId("review-dialog-submit-comment")).not.toBeDisabled();
    });
    expect(
      screen.getByTestId("review-dialog-submit-request-changes"),
    ).not.toBeDisabled();
  });

  it("submits APPROVE with empty body and closes the dialog on success", async () => {
    const user = userEvent.setup();
    submitMutateAsync.mockResolvedValueOnce({
      status: "succeeded",
      action_id: "act-1",
    });
    const onOpenChange = vi.fn();

    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={onOpenChange} />,
      { wrapper: Wrapper },
    );

    await user.click(screen.getByTestId("review-dialog-submit-approve"));

    await waitFor(() => {
      expect(submitMutateAsync).toHaveBeenCalledWith({
        event: "APPROVE",
        body: "",
      });
    });
    await waitFor(() => {
      expect(toastSpies.success).toHaveBeenCalled();
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });
  });

  it("renders the inline error banner when the server returns failed", async () => {
    const user = userEvent.setup();
    submitMutateAsync.mockResolvedValueOnce({
      status: "failed",
      action_id: "act-1",
      error: "Cannot approve your own pull request",
    });

    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );

    await user.click(screen.getByTestId("review-dialog-submit-approve"));

    await waitFor(() => {
      expect(screen.getByTestId("review-dialog-error")).toBeInTheDocument();
    });
    expect(
      screen.getByText(/Cannot approve your own pull request/i),
    ).toBeInTheDocument();
  });

  it("renders the error banner with the thrown ApiError message on rejection", async () => {
    const user = userEvent.setup();
    submitMutateAsync.mockRejectedValueOnce(new Error("rate limited"));

    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );

    await user.click(screen.getByTestId("review-dialog-submit-approve"));

    await waitFor(() => {
      expect(screen.getByText(/rate limited/i)).toBeInTheDocument();
    });
  });

  it("opens a confirmation dialog before firing Request changes", async () => {
    const user = userEvent.setup();
    submitMutateAsync.mockResolvedValueOnce({
      status: "succeeded",
      action_id: "act-1",
    });

    render(
      <ReviewDialog pr={makePR()} open={true} onOpenChange={() => {}} />,
      { wrapper: Wrapper },
    );

    const textarea = screen.getByLabelText(/Comment$/i);
    await user.type(textarea, "needs work");

    await user.click(
      screen.getByTestId("review-dialog-submit-request-changes"),
    );

    // First click opens the AlertDialog; the mutation does NOT fire yet.
    expect(submitMutateAsync).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/Request changes on this PR\?/i),
    ).toBeInTheDocument();

    // Confirm — the second "Request changes" button is the AlertDialog
    // action (the trigger button in the parent dialog stays mounted).
    const confirmButtons = screen.getAllByRole("button", {
      name: /Request changes/i,
    });
    await user.click(confirmButtons[confirmButtons.length - 1]!);

    await waitFor(() => {
      expect(submitMutateAsync).toHaveBeenCalledWith({
        event: "REQUEST_CHANGES",
        body: "needs work",
      });
    });
  });
});
