// @vitest-environment jsdom

// Where focus lands when the full log closes on the header-chip path. The
// trigger lives in the active-work popover while the chip itself renders the
// dialog, so the open modality has to survive that hop — the sibling suite
// stubs both the row and the dialog and cannot see it. Real popover, real
// dialog: focus return is the behaviour under test.

import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueAgentHeaderChip } from "./issue-agent-header-chip";

vi.mock("@multica/core/api", () => ({ api: {
  listTasksByIssue: vi.fn(), listTaskMessages: vi.fn(), cancelTask: vi.fn(),
}, dispatchReasonCode: () => undefined }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({
  getActorName: () => "Reviewer", getActorInitials: () => "RE", getActorAvatarUrl: () => null,
}) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../agents/components/agent-avatar-stack", () => ({ AgentAvatarStack: () => <span /> }));

// Stand in for the transcript body only; the dialog shell stays real so
// `finalFocus` reaches Base UI's focus manager as it does in the app.
vi.mock("../../common/task-transcript/agent-transcript-dialog", async () => {
  const { Dialog, DialogContent, DialogTitle } = await import("@multica/ui/components/ui/dialog");
  return {
    AgentTranscriptDialog: ({ open, onOpenChange, finalFocus }: {
      open: boolean; onOpenChange: (open: boolean) => void; finalFocus?: boolean;
    }) => (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent finalFocus={finalFocus}>
          <DialogTitle>Full transcript</DialogTitle>
        </DialogContent>
      </Dialog>
    ),
  };
});

const running: AgentTask = {
  id: "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc", agent_id: "agent", runtime_id: "runtime",
  issue_id: "issue-1", status: "running", priority: 0, created_at: "2026-09-07T00:00:00Z",
  started_at: "2026-09-07T00:00:00Z", dispatched_at: null, completed_at: null, result: null, error: null,
};

afterEach(() => { cleanup(); vi.clearAllMocks(); });

async function openActiveWorkPopover() {
  vi.mocked(api.listTasksByIssue).mockResolvedValue([running]);
  vi.mocked(api.listTaskMessages).mockResolvedValue([]);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const user = userEvent.setup();
  renderWithI18n(
    <QueryClientProvider client={client}><IssueAgentHeaderChip issueId="issue-1" /></QueryClientProvider>,
  );
  const chip = await screen.findByRole("button", { name: /working/i });
  chip.focus();
  await user.keyboard("{Enter}");
  return { user, trigger: await screen.findByRole("button", { name: "View transcript" }) };
}

// The log opens over the still-open popover, so the first Esc dismisses the
// popover and the second closes the log.
async function closeLog(user: ReturnType<typeof userEvent.setup>) {
  await user.keyboard("{Escape}");
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByText("Full transcript")).not.toBeInTheDocument());
}

describe("IssueAgentHeaderChip full log focus", () => {
  it("hands focus back to the row trigger after a keyboard open", async () => {
    const { user, trigger } = await openActiveWorkPopover();

    trigger.focus();
    await user.keyboard("{Enter}");
    await screen.findByText("Full transcript");
    await closeLog(user);

    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("leaves the row trigger alone after a pointer open", async () => {
    const { user, trigger } = await openActiveWorkPopover();

    await user.click(trigger);
    await screen.findByText("Full transcript");
    await closeLog(user);

    expect(trigger).not.toHaveFocus();
  });
});
