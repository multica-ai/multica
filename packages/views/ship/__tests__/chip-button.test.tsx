import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import { GitMerge, Bell } from "lucide-react";
import type { ActionResult, PullRequest } from "@multica/core/types";
import { ChipButton } from "../components/chip-button";
import type { PrChip } from "../hooks/use-pr-chips";

// Mock sonner so we can assert which toast variant fired without needing a
// Toaster mounted in the test tree.
const toastSpies = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
}));
vi.mock("sonner", () => ({
  toast: toastSpies,
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RESOURCES}>
      {children}
    </I18nProvider>
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
    review_decision: "APPROVED",
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

const mergeChip: PrChip = {
  id: "merge",
  action: "merge",
  labelKey: "merge",
  icon: GitMerge,
  variant: "primary",
  destructive: true,
};

const nudgeChip: PrChip = {
  id: "nudge_author",
  action: "nudge_author",
  labelKey: "nudge_author",
  icon: Bell,
  variant: "secondary",
};

beforeEach(() => {
  toastSpies.success.mockReset();
  toastSpies.error.mockReset();
  toastSpies.info.mockReset();
});

describe("ChipButton", () => {
  it("fires the mutation immediately for non-destructive chips", async () => {
    const user = userEvent.setup();
    const onFire = vi.fn<
      (body?: Record<string, unknown>) => Promise<ActionResult>
    >().mockResolvedValue({
      status: "succeeded",
      action_id: "act-1",
    });

    render(
      <ChipButton chip={nudgeChip} pr={makePR()} onFire={onFire} isPending={false} />,
      { wrapper: I18nWrapper },
    );

    await user.click(screen.getByRole("button", { name: /Nudge author/i }));

    // No dialog. Mutation fires once and the success toast is announced.
    expect(onFire).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(toastSpies.success).toHaveBeenCalledTimes(1);
    });
  });

  it("opens a confirmation dialog before firing destructive chips", async () => {
    const user = userEvent.setup();
    const onFire = vi.fn<
      (body?: Record<string, unknown>) => Promise<ActionResult>
    >().mockResolvedValue({
      status: "succeeded",
      action_id: "act-1",
    });

    render(
      <ChipButton chip={mergeChip} pr={makePR()} onFire={onFire} isPending={false} />,
      { wrapper: I18nWrapper },
    );

    await user.click(screen.getByRole("button", { name: /Merge/i }));

    // First click opens the dialog WITHOUT firing the mutation. The dialog
    // title comes from the per-action confirm_title key.
    expect(onFire).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/Merge this pull request\?/i),
    ).toBeInTheDocument();

    // Confirm the action — the second button in the dialog (after Cancel).
    const dialogButtons = screen.getAllByRole("button", { name: /Merge/i });
    // The trigger is still in the DOM; pick the dialog action which sits
    // inside the alert-dialog footer.
    await user.click(dialogButtons[dialogButtons.length - 1]!);

    expect(onFire).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(toastSpies.success).toHaveBeenCalledTimes(1);
    });
  });

  it("surfaces an in-progress toast for async actions", async () => {
    const user = userEvent.setup();
    const onFire = vi.fn<
      (body?: Record<string, unknown>) => Promise<ActionResult>
    >().mockResolvedValue({
      status: "in_progress",
      action_id: "act-1",
      agent_task_id: "task-99",
    });

    render(
      <ChipButton chip={nudgeChip} pr={makePR()} onFire={onFire} isPending={false} />,
      { wrapper: I18nWrapper },
    );

    await user.click(screen.getByRole("button", { name: /Nudge author/i }));

    await waitFor(() => {
      expect(toastSpies.info).toHaveBeenCalledTimes(1);
    });
  });

  it("surfaces the server error in the failure toast", async () => {
    const user = userEvent.setup();
    const onFire = vi.fn<
      (body?: Record<string, unknown>) => Promise<ActionResult>
    >().mockResolvedValue({
      status: "failed",
      action_id: "act-1",
      error: "branch is not mergeable",
    });

    render(
      <ChipButton chip={nudgeChip} pr={makePR()} onFire={onFire} isPending={false} />,
      { wrapper: I18nWrapper },
    );

    await user.click(screen.getByRole("button", { name: /Nudge author/i }));

    await waitFor(() => {
      expect(toastSpies.error).toHaveBeenCalledWith("branch is not mergeable");
    });
  });
});
