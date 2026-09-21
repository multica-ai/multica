// @vitest-environment jsdom

// Where focus lands when the full log closes. Kept apart from
// inline-comment-run.test.tsx because these cases need the real Base UI dialog
// — the focus return is the behaviour under test, so a stubbed dialog would
// assert nothing.

import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun } from "./inline-comment-run";

vi.mock("@multica/core/api", () => ({ api: {
  getIssue: vi.fn(), listTaskMessages: vi.fn(), cancelTask: vi.fn(), rerunIssue: vi.fn(),
}, dispatchReasonCode: () => undefined }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../editor", () => ({ ReadonlyContent: () => <div /> }));

// Stand in for the transcript body only. The dialog shell stays real so
// `finalFocus` reaches Base UI's focus manager exactly as it does in the app.
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
    StepBody: () => null,
  };
});

const id = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";
const completed: AgentTask = {
  id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "completed", priority: 0,
  created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
  completed_at: "2026-09-07T00:01:23Z", result: null, error: null,
};

afterEach(() => { cleanup(); vi.clearAllMocks(); });

function renderHeaderRun() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const wrap = (children: ReactNode) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  renderWithI18n(wrap(
    <InlineCommentRun run={{ task: completed, commentId: "comment", hasReply: true }} presentation="header" />,
  ));
  return screen.getByRole("button", { name: "Open full log" });
}

describe("InlineCommentRun full log focus", () => {
  it("leaves the trigger alone after a pointer open, so Esc reveals no ring or tooltip", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const user = userEvent.setup();
    const trigger = renderHeaderRun();

    await user.click(trigger);
    await screen.findByRole("dialog");
    await user.keyboard("{Escape}");

    // No focus on the trigger is what keeps its ring, its tooltip and the
    // hover-revealed action row it sits in from coming back with the dialog.
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(trigger).not.toHaveFocus();
  });

  it("hands focus back to the trigger after a keyboard open", async () => {
    vi.mocked(api.listTaskMessages).mockResolvedValue([]);
    const user = userEvent.setup();
    const trigger = renderHeaderRun();

    trigger.focus();
    await user.keyboard("{Enter}");
    await screen.findByRole("dialog");
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    await waitFor(() => expect(trigger).toHaveFocus());
  });
});
