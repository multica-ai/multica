import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses";
import type { InboxItem, IssueStatusEntry } from "@multica/core/types";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { InboxListItem } from "./inbox-list-item";

// The catalog is server state; these suites render leaves without a
// QueryClientProvider, so it is stubbed like the other data hooks. The real
// `buildIssueStatusCatalog` is used rather than a hand-rolled object so the
// resolver semantics under test (category, entry, is_system) are the shipped
// ones. `undefined` is the cold-catalog case every test but the status suite
// exercises.
let catalogEntries: IssueStatusEntry[] | undefined;

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog(catalogEntries),
}));

vi.mock("../../issues/components", () => ({
  StatusIcon: ({
    status,
    category,
    color,
  }: {
    status: string;
    category?: string;
    color?: string | null;
  }) => (
    <span
      data-testid="status-icon"
      data-status={status}
      data-category={category ?? ""}
      data-color={color ?? ""}
    />
  ),
}));
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

describe("InboxListItem keyboard semantics", () => {
  it("is a role=button host, so its own controls stay reachable", () => {
    // Interactive descendants of a real <button> are invalid HTML and are not
    // exposed to screen readers — the row carries an action button and a menu.
    const { container } = renderRow({ item: item(), view: "inbox" });

    const row = screen.getByTestId("actor-avatar").closest('[role="button"]')!;
    expect(row.tagName).toBe("DIV");
    expect(row.getAttribute("tabindex")).toBe("0");
    expect(container.querySelector("button")).not.toBeNull();
  });

  it("gates the archive affordance on hover capability, not viewport width", () => {
    // A width breakpoint hides this button on every wide surface, touch or
    // not. On a pointer that cannot hover — a phone in landscape clears `md` —
    // that left the row with no reachable archive at all, which is the whole
    // problem the compact menu exists to solve.
    const { container } = renderRow({ item: item(), view: "inbox" });

    const actionButton = container.querySelector("button")!;

    expect(actionButton.className).toContain(
      "[@media(hover:hover)]:group-hover:inline-flex",
    );
    expect(actionButton.className).not.toMatch(/(^|\s)(sm|md|lg|xl|2xl):group-/);
  });

  it("activates on Enter like the button it replaces", () => {
    const onClick = vi.fn();
    renderRow({ item: item(), view: "inbox", onClick });

    const row = screen.getByTestId("actor-avatar").closest('[role="button"]')!;
    fireEvent.keyDown(row, { key: "Enter" });

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("leaves keys pressed inside its own controls alone", () => {
    // Enter on the archive button must archive, not select the row.
    const onClick = vi.fn();
    const { container } = renderRow({ item: item(), view: "inbox", onClick });

    fireEvent.keyDown(container.querySelector("button")!, { key: "Enter" });

    expect(onClick).not.toHaveBeenCalled();
  });

  it("runs the row action without selecting the row", () => {
    const onClick = vi.fn();
    const onAction = vi.fn();
    const { container } = render(
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={makeAdapter()}>
          <InboxListItem
            item={item()}
            view="inbox"
            isSelected={false}
            onClick={onClick}
            onAction={onAction}
          />
        </NavigationProvider>
      </WorkspaceSlugProvider>,
    );

    fireEvent.click(container.querySelector("button")!);

    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("InboxListItem link semantics", () => {
  it("plain click keeps the master-detail selection and does not navigate", () => {
    const onClick = vi.fn();
    const push = vi.fn();
    renderRow({ item: item(), view: "inbox", onClick, adapter: makeAdapter({ push }) });

    fireEvent.click(screen.getByTestId("actor-avatar").closest('[role="button"]')!);
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

    fireEvent.click(screen.getByTestId("actor-avatar").closest('[role="button"]')!, {
      metaKey: true,
    });
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", undefined);
    expect(onClick).not.toHaveBeenCalled();
  });

  it("middle click opens the referenced issue in a background tab", () => {
    const openInNewTab = vi.fn();
    renderRow({ item: item(), view: "inbox", adapter: makeAdapter({ openInNewTab }) });

    const row = screen.getByTestId("actor-avatar").closest('[role="button"]')!;
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

    fireEvent.click(screen.getByTestId("actor-avatar").closest('[role="button"]')!, {
      metaKey: true,
    });
    expect(onClick).toHaveBeenCalledTimes(1);
    expect(openInNewTab).not.toHaveBeenCalled();
  });
});


// ---------------------------------------------------------------------------
// MUL-6395 — the row's only status affordance is one glyph, and the glyph set
// is per CATEGORY. Without the status's own colour, switching an issue between
// two statuses that share a category (built-in "In Review" → custom "Human
// Review") repainted the row identically, so the inbox looked like it had
// simply not picked the change up.
// ---------------------------------------------------------------------------

const IN_REVIEW_BUILT_IN: IssueStatusEntry = {
  id: "in_review",
  workspace_id: "workspace-1",
  key: "in_review",
  name: "In Review",
  description: "",
  category: "in_review",
  color: "#8b5cf6",
  is_system: true,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

const HUMAN_REVIEW: IssueStatusEntry = {
  ...IN_REVIEW_BUILT_IN,
  id: "human_review",
  key: "human_review",
  name: "Human Review",
  color: "#ff0000",
  is_system: false,
  position: 1,
};

describe("InboxListItem status glyph", () => {
  it("paints a custom status in its own colour", () => {
    catalogEntries = [IN_REVIEW_BUILT_IN, HUMAN_REVIEW];

    const { getByTestId } = renderRow({
      item: item({ issue_status: "human_review" }),
      view: "inbox",
    });

    const icon = getByTestId("status-icon");
    expect(icon.getAttribute("data-category")).toBe("in_review");
    expect(icon.getAttribute("data-color")).toBe("#ff0000");
  });

  it("leaves a built-in status on its semantic token colour", () => {
    // The catalog seeds a colour for the built-ins too, but those are theme
    // tokens in the UI — passing the seeded hex would hard-code one theme.
    catalogEntries = [IN_REVIEW_BUILT_IN, HUMAN_REVIEW];

    const { getByTestId } = renderRow({
      item: item({ issue_status: "in_review" }),
      view: "inbox",
    });

    expect(getByTestId("status-icon").getAttribute("data-color")).toBe("");
  });

  it("names the status, so two statuses in one category stay distinguishable", () => {
    catalogEntries = [IN_REVIEW_BUILT_IN, HUMAN_REVIEW];

    const { container } = renderRow({
      item: item({ issue_status: "human_review" }),
      view: "inbox",
    });

    expect(container.querySelector('[title="Human Review"]')).not.toBeNull();
  });

  it("still renders a custom status before the catalog lands", () => {
    // categoryOf falls back to `todo` for an unresolved key; the row must show
    // the glyph anyway rather than dropping the status entirely.
    catalogEntries = undefined;

    const { getByTestId } = renderRow({
      item: item({ issue_status: "human_review" }),
      view: "inbox",
    });

    const icon = getByTestId("status-icon");
    expect(icon.getAttribute("data-status")).toBe("human_review");
    expect(icon.getAttribute("data-color")).toBe("");
  });
});
