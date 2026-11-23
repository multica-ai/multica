import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { createQueryClient } from "@multica/core/query-client";
import type { Agent, Issue, IssueSubscriber } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

import { InboxMoreMenu } from "./inbox-more-menu";

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
  const onToggleRead = vi.fn();
  renderWithI18n(
    <QueryClientProvider client={createQueryClient()}>
      <InboxMoreMenu issueId={ISSUE_ID} userId="me" read onToggleRead={onToggleRead} onUnsubscribe={onConfirm} />
    </QueryClientProvider>,
  );
  return { onConfirm, onToggleRead };
}

const openMenu = () => fireEvent.click(screen.getByRole("button", { name: "More notification actions" }));

// Without sub-issues the item acts directly, once the child count is known;
// until then it asks for the scope.
async function directItem() {
  openMenu();
  return screen.findByRole("menuitem", { name: /^Unsubscribe & archive/ });
}

describe("InboxMoreMenu", () => {
  it("marks the notification unread", async () => {
    const { onToggleRead } = setup();
    openMenu();
    fireEvent.click(await screen.findByRole("menuitem", { name: /^Mark as unread/ }));
    expect(onToggleRead).toHaveBeenCalledTimes(1);
  });

  it("leaves just this issue on an issue that is someone else's", async () => {
    const { onConfirm } = setup();
    fireEvent.click(await directItem());
    expect(onConfirm).toHaveBeenCalledWith("issue");
  });

  it("runs on Shift+E", async () => {
    const { onConfirm } = setup();
    await directItem();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    fireEvent.keyDown(document.body, { key: "E", code: "KeyE", shiftKey: true });
    expect(onConfirm).toHaveBeenCalledWith("issue");
  });

  it("asks for the scope when the issue has sub-issues, and opens on it with Shift+E", async () => {
    const { onConfirm } = setup({ children: [{ id: "c1" }, { id: "c2" }] });
    await waitFor(() => {
      fireEvent.keyDown(document.body, { key: "E", code: "KeyE", shiftKey: true });
      expect(screen.getByRole("menu")).toBeInTheDocument();
    });
    fireEvent.click(await screen.findByText("This issue and its 2 sub-issues"));
    expect(onConfirm).toHaveBeenCalledWith("subtree");
  });

  it.each([
    ["assigned to the viewer", { assignee_type: "member", assignee_id: "me" }],
    ["assigned to the viewer's agent", { assignee_type: "agent", assignee_id: "agent-mine" }],
  ] as const)("does not offer unsubscribing on an issue %s", async (_, assignee) => {
    setup({ assignee });
    await new Promise((r) => setTimeout(r, 50));
    openMenu();
    await screen.findByRole("menuitem", { name: /^Mark as unread/ });
    expect(screen.queryByRole("menuitem", { name: /Unsubscribe/ })).toBeNull();
  });

  it("does not offer unsubscribing when the viewer is not subscribed", async () => {
    setup({ subscribed: false });
    await new Promise((r) => setTimeout(r, 50));
    openMenu();
    await screen.findByRole("menuitem", { name: /^Mark as unread/ });
    expect(screen.queryByRole("menuitem", { name: /Unsubscribe/ })).toBeNull();
  });
});
