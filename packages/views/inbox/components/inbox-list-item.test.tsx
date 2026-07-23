import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxListItem } from "./inbox-list-item";

vi.mock("../../issues/components", () => ({ StatusIcon: () => null }));
vi.mock("../../issues/components/issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: ({ issueId }: { issueId: string }) => (
    <span data-testid="issue-agent-activity" data-issue-id={issueId} />
  ),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({
    actorType,
    actorId,
    showStatusDot,
  }: {
    actorType: string;
    actorId: string;
    showStatusDot?: boolean;
  }) => (
    <span
      data-testid="actor-avatar"
      data-actor-type={actorType}
      data-actor-id={actorId}
      data-show-status-dot={showStatusDot === true ? "true" : "false"}
    />
  ),
}));
vi.mock("./inbox-detail-label", () => ({ InboxDetailLabel: () => null }));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "label" }) }));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function renderRow(props: { item: InboxItem; view: "inbox" | "archived" }) {
  return render(
    <InboxListItem
      item={props.item}
      view={props.view}
      isSelected={false}
      onClick={vi.fn()}
      onAction={vi.fn()}
    />,
  );
}

const unreadDot = (container: HTMLElement) => container.querySelector(".bg-brand");
const title = (container: HTMLElement) => container.querySelector(".truncate");

describe("InboxListItem unread affordance", () => {
  it("marks an unread row in the main inbox", () => {
    const { container } = renderRow({ item: item({ read: false }), view: "inbox" });

    expect(unreadDot(container)).not.toBeNull();
    expect(title(container)?.className).toContain("font-medium");
  });

  it("leaves a read row unmarked in the main inbox", () => {
    const { container } = renderRow({ item: item({ read: true }), view: "inbox" });

    expect(unreadDot(container)).toBeNull();
    expect(title(container)?.className).not.toContain("font-medium");
  });

  it("renders an unread row as read in the archived view", () => {
    // Archiving preserves `read` so unarchiving can restore real unread state,
    // which left archived rows showing a dot no action in this view can clear.
    const { container } = renderRow({
      item: item({ read: false, archived: true }),
      view: "archived",
    });

    expect(unreadDot(container)).toBeNull();
    expect(title(container)?.className).not.toContain("font-medium");
  });
});

describe("InboxListItem issue activity", () => {
  it("shows issue-specific agent activity without an availability dot", () => {
    const { getByTestId } = renderRow({ item: item(), view: "inbox" });

    expect(getByTestId("actor-avatar").getAttribute("data-show-status-dot")).toBe(
      "false",
    );
    expect(
      getByTestId("issue-agent-activity").getAttribute("data-issue-id"),
    ).toBe("issue-1");
  });

  it("omits issue activity for a notification without an issue", () => {
    const { queryByTestId } = renderRow({
      item: item({ issue_id: null }),
      view: "inbox",
    });

    expect(queryByTestId("issue-agent-activity")).toBeNull();
  });
});
