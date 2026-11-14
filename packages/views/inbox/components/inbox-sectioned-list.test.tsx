import { forwardRef, useImperativeHandle } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useInboxSectionStore } from "@multica/core/inbox/section-store";
import type {
  InboxSectionRow,
  InboxSections,
  InboxWorkingRow,
} from "@multica/core/inbox/sections";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { InboxSectionedList } from "./inbox-sectioned-list";

// jsdom has no layout, so the real Virtuoso measures a 0-height viewport and
// renders nothing. The mock renders every entry inline and exposes the handle
// method the list drives, so the keyboard behaviour is observable.
const scrollIntoView = vi.hoisted(() => vi.fn());

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    {
      data,
      itemContent,
      computeItemKey,
    }: {
      data: unknown[];
      itemContent: (index: number, entry: unknown) => React.ReactNode;
      computeItemKey: (index: number, entry: unknown) => string;
    },
    ref: React.Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => ({ scrollIntoView }));
    return (
      <div>
        {data.map((entry, index) => (
          <div key={computeItemKey(index, entry)} data-entry={computeItemKey(index, entry)}>
            {itemContent(index, entry)}
          </div>
        ))}
      </div>
    );
  }),
}));

// The rows render avatars, hover cards and queries of their own, none of which
// this file is about. Each is a bare button carrying what the list hands it.
vi.mock("./inbox-list-item", () => ({
  InboxListItem: ({
    item,
    section,
    isSelected,
    onClick,
  }: {
    item: InboxItem;
    section: string;
    isSelected: boolean;
    onClick: () => void;
  }) => (
    <button type="button" data-row={section} data-selected={isSelected} onClick={onClick}>
      {item.id}
    </button>
  ),
}));
vi.mock("./inbox-working-item", () => ({
  InboxWorkingItem: ({
    row,
    isSelected,
    onClick,
  }: {
    row: InboxWorkingRow;
    isSelected: boolean;
    onClick: () => void;
  }) => (
    <button type="button" data-row="working" data-selected={isSelected} onClick={onClick}>
      {row.issueId}
    </button>
  ),
}));

function row(id: string, overrides: Partial<InboxItem> = {}): InboxSectionRow {
  const item: InboxItem = {
    id,
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: `issue-${id}`,
    title: "Issue title",
    body: null,
    issue_status: null,
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
  return { item, reason: null, snoozedUntil: null, preview: null };
}

const working = (issueId: string, agentId = "agent-1"): InboxWorkingRow => ({
  issueId,
  taskId: `task-${issueId}`,
  agentId,
  queued: false,
  since: "2026-06-15T08:00:00Z",
});

const EMPTY: InboxSections = { needsYou: [], working: [], updates: [], snoozed: [], readUpdates: [] };

const FULL: InboxSections = {
  needsYou: [row("a"), row("b")],
  working: [working("issue-w")],
  updates: [row("u", { read: false })],
  snoozed: [row("s")],
  readUpdates: [],
};

function renderList(
  sections: InboxSections,
  props: Partial<React.ComponentProps<typeof InboxSectionedList>> = {},
) {
  const onSelect = vi.fn();
  const onSelectIssue = vi.fn();
  const utils = renderWithI18n(
    <InboxSectionedList
      sections={sections}
      selectedKey=""
      onSelect={onSelect}
      onSelectIssue={onSelectIssue}
      onAction={vi.fn()}
      {...props}
    />,
  );
  const scroller = utils.container.querySelector(".overflow-y-auto") as HTMLElement;
  return { ...utils, scroller, onSelect, onSelectIssue };
}

const header = (section: string) =>
  document.querySelector(`[data-section-header="${section}"]`) as HTMLElement | null;
const rows = () =>
  Array.from(document.querySelectorAll("[data-row]")).map(
    (el) => `${el.getAttribute("data-row")}:${el.textContent}`,
  );

beforeEach(() => {
  scrollIntoView.mockClear();
  useInboxSectionStore.setState({ open: { updates: false, snoozed: false } });
});

describe("InboxSectionedList sections", () => {
  it("lists what needs the user, then what agents are working on, and folds the rest", () => {
    renderList(FULL);

    expect(
      Array.from(document.querySelectorAll("[data-section-header]")).map((el) =>
        el.getAttribute("data-section-header"),
      ),
    ).toEqual(["needs_you", "working", "updates", "snoozed"]);
    expect(rows()).toEqual(["needs_you:a", "needs_you:b", "working:issue-w"]);
    expect(header("needs_you")).toHaveTextContent(/^Needs you2$/);
    expect(header("working")).toHaveTextContent(/^Working1$/);
    expect(header("updates")).toHaveTextContent(/^Updates1$/);
    expect(header("snoozed")).toHaveTextContent(/^Snoozed1$/);
  });

  it("clears the updates from the header once they are in view", () => {
    const onClearUpdates = vi.fn();
    renderList(FULL, { onClearUpdates });
    // Folded, nothing is listed to clear.
    expect(screen.queryByRole("button", { name: "Clear" })).toBeNull();

    fireEvent.click(within(header("updates")!).getByRole("button", { name: /Updates/ }));
    fireEvent.click(within(header("updates")!).getByRole("button", { name: "Clear" }));

    expect(onClearUpdates).toHaveBeenCalledOnce();
    // Clearing is not folding.
    expect(useInboxSectionStore.getState().open.updates).toBe(true);
    expect(within(header("snoozed")!).queryByRole("button", { name: "Clear" })).toBeNull();
  });

  it("offers no Clear where the list is only a preview", () => {
    useInboxSectionStore.setState({ open: { updates: true, snoozed: false } });
    renderList(FULL);

    expect(screen.queryByRole("button", { name: "Clear" })).toBeNull();
  });

  it("opens a folded section from its header and remembers it", () => {
    renderList(FULL);
    const toggle = within(header("updates")!).getByRole("button");
    expect(toggle).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(toggle);

    expect(rows()).toEqual(["needs_you:a", "needs_you:b", "working:issue-w", "updates:u"]);
    expect(within(header("updates")!).getByRole("button")).toHaveAttribute("aria-expanded", "true");
    expect(useInboxSectionStore.getState().open).toEqual({ updates: true, snoozed: false });
  });

  it("never folds what needs the user or what is being worked on", () => {
    renderList(FULL);

    expect(within(header("needs_you")!).queryByRole("button")).toBeNull();
    expect(within(header("working")!).queryByRole("button")).toBeNull();
  });

  it("leaves out a section with nothing in it, except the one that leads", () => {
    renderList({ ...EMPTY, updates: [row("u")] });

    expect(header("needs_you")).not.toBeNull();
    expect(header("working")).toBeNull();
    expect(header("snoozed")).toBeNull();
  });
});

describe("InboxSectionedList with nothing waiting", () => {
  it("says so, and counts the agents still at work", () => {
    renderList({
      ...EMPTY,
      working: [working("issue-1", "agent-1"), working("issue-2", "agent-1"), working("issue-3", "agent-2")],
    });

    expect(screen.getByText("All caught up")).toBeInTheDocument();
    expect(
      screen.getByText("2 agents are still working. Results come back here."),
    ).toBeInTheDocument();
    // The section has nothing to count.
    expect(header("needs_you")).toHaveTextContent(/^Needs you$/);
  });

  it("says nothing about agents when none is working", () => {
    renderList(EMPTY);

    expect(screen.getByText("All caught up")).toBeInTheDocument();
    expect(screen.queryByText(/still working/)).toBeNull();
  });
});

describe("InboxSectionedList under a filter", () => {
  it("lists the matches: folded sections open, empty ones gone", () => {
    renderList({ ...EMPTY, updates: [row("u")], snoozed: [row("s")] }, { filtered: true });

    expect(header("needs_you")).toBeNull();
    expect(screen.queryByText("All caught up")).toBeNull();
    expect(rows()).toEqual(["updates:u", "snoozed:s"]);
    // Nothing to fold while the list is the answer to a filter.
    expect(within(header("updates")!).queryByRole("button")).toBeNull();
  });

  it("shows the empty label and its action when nothing matches", () => {
    renderList(EMPTY, {
      filtered: true,
      emptyLabel: "No notifications match these filters",
      emptyAction: <button type="button">Clear filters</button>,
    });

    expect(screen.getByText("No notifications match these filters")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Clear filters" })).toBeInTheDocument();
  });
});

describe("InboxSectionedList selection", () => {
  it("selects a notification by its issue and a working row by its own", () => {
    const { onSelect, onSelectIssue } = renderList(FULL, { selectedKey: "issue-w" });

    expect(screen.getByText("issue-w")).toHaveAttribute("data-selected", "true");
    expect(screen.getByText("a")).toHaveAttribute("data-selected", "false");

    fireEvent.click(screen.getByText("b"));
    expect(onSelect).toHaveBeenCalledWith(FULL.needsYou[1]!.item);
    fireEvent.click(screen.getByText("issue-w"));
    expect(onSelectIssue).toHaveBeenCalledWith("issue-w");
  });

  it("walks the arrow keys across sections, past their headers", () => {
    const { scroller, onSelectIssue } = renderList(FULL, { selectedKey: "issue-b" });

    const claimed = !fireEvent.keyDown(scroller, { key: "ArrowDown" });

    expect(claimed).toBe(true);
    expect(onSelectIssue).toHaveBeenCalledWith("issue-w");
    // Entries: header, a, b, header, the working row.
    expect(scrollIntoView).toHaveBeenCalledWith({ index: 4 });
  });

  it("stops at the last row that is showing, not inside a folded section", () => {
    const { scroller, onSelect, onSelectIssue } = renderList(FULL, { selectedKey: "issue-w" });

    fireEvent.keyDown(scroller, { key: "ArrowDown" });

    expect(onSelect).not.toHaveBeenCalled();
    expect(onSelectIssue).not.toHaveBeenCalled();
  });

  it("reaches an opened section's rows", () => {
    useInboxSectionStore.setState({ open: { updates: true, snoozed: false } });
    const { scroller, onSelect } = renderList(FULL, { selectedKey: "issue-w" });

    fireEvent.keyDown(scroller, { key: "ArrowDown" });

    expect(onSelect).toHaveBeenCalledWith(FULL.updates[0]!.item);
  });

  it("leaves modified arrows and arrows typed into a field alone", () => {
    const { scroller, onSelect } = renderList(FULL, { selectedKey: "issue-a" });

    expect(fireEvent.keyDown(scroller, { key: "ArrowDown", metaKey: true })).toBe(true);
    expect(onSelect).not.toHaveBeenCalled();
  });
});
