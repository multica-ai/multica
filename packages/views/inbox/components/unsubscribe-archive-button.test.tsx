import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { createQueryClient } from "@multica/core/query-client";
import type { Agent, Issue, IssueSubscriber } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

import { UnsubscribeArchiveButton } from "./unsubscribe-archive-button";

const ISSUE_ID = "issue-1";

function setup({
  assignee = { assignee_type: "member", assignee_id: "someone" } as Partial<Issue>,
  subscribed = true,
  children = [] as Partial<Issue>[],
} = {}) {
  const issue = { id: ISSUE_ID, workspace_id: "ws-1", ...assignee } as Issue;
  const subscribers: IssueSubscriber[] = subscribed
    ? [{ issue_id: ISSUE_ID, user_type: "member", user_id: "me", reason: "commenter", created_at: "" }]
    : [];
  const agents = [{ id: "agent-mine", owner_id: "me" }] as Agent[];
  setApiInstance({
    getIssue: vi.fn(async () => issue),
    listAgents: vi.fn(async () => agents),
    listIssueSubscribers: vi.fn(async () => subscribers),
    listChildIssues: vi.fn(async () => ({ issues: children })),
  } as unknown as ApiClient);
  const onConfirm = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={createQueryClient()}>
      <UnsubscribeArchiveButton issueId={ISSUE_ID} userId="me" onConfirm={onConfirm} />
    </QueryClientProvider>,
  );
  return { onConfirm };
}

// Without sub-issues the button acts directly, once the child count is known;
// until then it is the scope menu's trigger.
async function directButton() {
  return waitFor(() => {
    const button = screen.getByRole("button", { name: "Unsubscribe & archive" });
    expect(button).not.toHaveAttribute("aria-haspopup");
    return button;
  });
}

describe("UnsubscribeArchiveButton", () => {
  it("leaves just this issue on an issue that is someone else's", async () => {
    const { onConfirm } = setup();
    fireEvent.click(await directButton());
    expect(onConfirm).toHaveBeenCalledWith("issue");
  });

  it("runs on Shift+E", async () => {
    const { onConfirm } = setup();
    await directButton();
    fireEvent.keyDown(document.body, { key: "E", code: "KeyE", shiftKey: true });
    expect(onConfirm).toHaveBeenCalledWith("issue");
  });

  it("asks for the scope when the issue has sub-issues", async () => {
    const { onConfirm } = setup({ children: [{ id: "c1" }, { id: "c2" }] });
    const trigger = await screen.findByRole("button", { name: "Unsubscribe & archive" });
    await waitFor(() => expect(trigger).toHaveAttribute("aria-haspopup"));
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("This issue and its 2 sub-issues"));
    expect(onConfirm).toHaveBeenCalledWith("subtree");
  });

  it.each([
    ["assigned to the viewer", { assignee_type: "member", assignee_id: "me" }],
    ["assigned to the viewer's agent", { assignee_type: "agent", assignee_id: "agent-mine" }],
  ] as const)("is not offered on an issue %s", async (_, assignee) => {
    setup({ assignee });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("button", { name: "Unsubscribe & archive" })).toBeNull();
  });

  it("is not offered when the viewer is not subscribed", async () => {
    setup({ subscribed: false });
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByRole("button", { name: "Unsubscribe & archive" })).toBeNull();
  });
});
