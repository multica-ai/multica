import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { InboxItem } from "@multica/core/types";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { InboxListItem } from "./inbox-list-item";

vi.mock("../../issues/components", () => ({ StatusIcon: () => null }));
vi.mock("../../issues/components/issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: ({
    issueId,
    hoverCard,
  }: {
    issueId: string;
    hoverCard?: boolean;
  }) => (
    <span
      data-testid="issue-agent-activity"
      data-issue-id={issueId}
      data-hover-card={hoverCard === false ? "false" : "true"}
    />
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

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderRow(props: {
  item: InboxItem;
  view: "inbox" | "archived";
  adapter?: NavigationAdapter;
  onClick?: () => void;
}) {
  return render(
    <WorkspaceSlugProvider slug="acme">
      <NavigationProvider value={props.adapter ?? makeAdapter()}>
        <InboxListItem
          item={props.item}
          view={props.view}
          isSelected={false}
          onClick={props.onClick ?? vi.fn()}
          onAction={vi.fn()}
        />
      </NavigationProvider>
    </WorkspaceSlugProvider>,
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

  it("shows the activity badge without its hover card", () => {
    // Triage rows only need "an agent is on this". The card behind the badge
    // adds elapsed time, which does not change whether you open the row, and
    // the row already carries the actor hover card on the left.
    const { getByTestId } = renderRow({ item: item(), view: "inbox" });

    expect(
      getByTestId("issue-agent-activity").getAttribute("data-hover-card"),
    ).toBe("false");
  });

  it("omits issue activity for a notification without an issue", () => {
    const { queryByTestId } = renderRow({
      item: item({ issue_id: null }),
      view: "inbox",
    });

    expect(queryByTestId("issue-agent-activity")).toBeNull();
  });
});

describe("InboxListItem link semantics", () => {
  it("plain click keeps the master-detail selection and does not navigate", () => {
    const onClick = vi.fn();
    const push = vi.fn();
    renderRow({ item: item(), view: "inbox", onClick, adapter: makeAdapter({ push }) });

    fireEvent.click(screen.getByTestId("actor-avatar").closest("button")!);
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(push).not.toHaveBeenCalled();
  });

  it("cmd-click opens the referenced issue in a background tab (desktop)", () => {
    const onClick = vi.fn();
    const openInNewTab = vi.fn();
    renderRow({
      item: item(),
      view: "inbox",
      onClick,
      adapter: makeAdapter({ openInNewTab }),
    });

    fireEvent.click(screen.getByTestId("actor-avatar").closest("button")!, {
      metaKey: true,
    });
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", undefined);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("middle click opens the referenced issue in a background tab", () => {
    const openInNewTab = vi.fn();
    renderRow({ item: item(), view: "inbox", adapter: makeAdapter({ openInNewTab }) });

    const row = screen.getByTestId("actor-avatar").closest("button")!;
    const event = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    row.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", undefined);
  });

  it("cmd-click on a row without an issue falls back to plain selection", () => {
    const onClick = vi.fn();
    const openInNewTab = vi.fn();
    renderRow({
      item: item({ issue_id: null }),
      view: "inbox",
      onClick,
      adapter: makeAdapter({ openInNewTab }),
    });

    fireEvent.click(screen.getByTestId("actor-avatar").closest("button")!, {
      metaKey: true,
    });
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(openInNewTab).not.toHaveBeenCalled();
  });
});
