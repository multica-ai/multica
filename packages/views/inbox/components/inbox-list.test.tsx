import { forwardRef, useImperativeHandle } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxList } from "./inbox-list";

// jsdom has no layout, so the real Virtuoso measures a 0-height viewport and
// renders nothing. The mock renders every row inline and exposes the handle
// methods the list drives, so the keyboard behaviour is observable.
const scrollIntoView = vi.hoisted(() => vi.fn());

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    {
      data,
      itemContent,
    }: {
      data: InboxItem[];
      itemContent: (index: number, item: InboxItem) => React.ReactNode;
    },
    ref: React.Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => ({ scrollIntoView }));
    return (
      <div>
        {data.map((item, index) => (
          <div key={item.id}>{itemContent(index, item)}</div>
        ))}
      </div>
    );
  }),
}));

// The row renders avatars, hover cards, and a status icon — none of which this
// file is about. Keep it a bare button carrying the two things the list reads.
vi.mock("./inbox-list-item", () => ({
  InboxListItem: ({
    item,
    isSelected,
    onClick,
  }: {
    item: InboxItem;
    isSelected: boolean;
    onClick: () => void;
  }) => (
    <button type="button" data-selected={isSelected} onClick={onClick}>
      {item.id}
    </button>
  ),
}));

vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Inbox" }) }));

function item(id: string, overrides: Partial<InboxItem> = {}): InboxItem {
  return {
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
}

const items = [item("a"), item("b"), item("c")];

function renderList(selectedKey: string, onSelect = vi.fn()) {
  const utils = render(
    <InboxList
      items={items}
      view="inbox"
      selectedKey={selectedKey}
      archivedCount={0}
      onSelect={onSelect}
      onAction={vi.fn()}
      onOpenArchived={vi.fn()}
    />,
  );
  // The scroll container owns the key handler; it is the row's closest
  // ancestor with an overflow style.
  const scroller = utils.container.querySelector(".overflow-y-auto") as HTMLElement;
  return { ...utils, scroller, onSelect };
}

/** Press a key on the list, reporting whether the native scroll was claimed. */
function press(scroller: HTMLElement, key: string, init: KeyboardEventInit = {}) {
  return !fireEvent.keyDown(scroller, { key, ...init });
}

beforeEach(() => {
  scrollIntoView.mockClear();
});

describe("InboxList keyboard navigation", () => {
  it("moves the selection down instead of scrolling", () => {
    const { scroller, onSelect } = renderList("issue-a");

    const prevented = press(scroller, "ArrowDown");

    expect(onSelect).toHaveBeenCalledWith(items[1]);
    expect(prevented).toBe(true);
  });

  it("moves the selection up", () => {
    const { scroller, onSelect } = renderList("issue-b");

    press(scroller, "ArrowUp");

    expect(onSelect).toHaveBeenCalledWith(items[0]);
  });

  it("enters the list from either end when nothing is selected", () => {
    const down = renderList("");
    press(down.scroller, "ArrowDown");
    expect(down.onSelect).toHaveBeenCalledWith(items[0]);

    const up = renderList("");
    press(up.scroller, "ArrowUp");
    expect(up.onSelect).toHaveBeenCalledWith(items[2]);
  });

  it("stops at the ends of the list and still claims the key", () => {
    // Falling through to the native scroll at the last row would move the
    // viewport away from the row that stays selected.
    const { scroller, onSelect } = renderList("issue-c");

    const prevented = press(scroller, "ArrowDown");

    expect(onSelect).not.toHaveBeenCalled();
    expect(prevented).toBe(true);
  });

  it("scrolls the newly selected row into view", () => {
    // Virtuoso's own scrollIntoView, so a row that virtualization has not
    // mounted still gets there — and only when it is off-screen.
    const { scroller } = renderList("issue-a");

    press(scroller, "ArrowDown");

    expect(scrollIntoView).toHaveBeenCalledWith({ index: 1 });
  });

  it("leaves modified arrow keys alone", () => {
    // Shift+Down extends a selection and Alt/Cmd+Down are OS/browser scroll
    // accelerators; none of them mean "next notification".
    const { scroller, onSelect } = renderList("issue-a");

    expect(press(scroller, "ArrowDown", { shiftKey: true })).toBe(false);
    expect(press(scroller, "ArrowDown", { metaKey: true })).toBe(false);
    expect(press(scroller, "ArrowDown", { altKey: true })).toBe(false);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("leaves the key to a text field inside the list", () => {
    const { scroller, onSelect } = renderList("issue-a");
    const input = document.createElement("input");
    scroller.appendChild(input);

    press(input, "ArrowDown");

    expect(onSelect).not.toHaveBeenCalled();
  });

  it("ignores an arrow key that composition already owns", () => {
    // During IME composition the arrow key walks the candidate list.
    const { scroller, onSelect } = renderList("issue-a");

    press(scroller, "ArrowDown", { keyCode: 229 });

    expect(onSelect).not.toHaveBeenCalled();
  });

  it("focuses the container on click so the arrow keys work right after", () => {
    // Safari does not focus a <button> on click, and virtualization can unmount
    // the clicked row — either way the keydown would stop reaching the list.
    const { scroller, onSelect } = renderList("");

    fireEvent.click(screen.getByText("b"));

    expect(onSelect).toHaveBeenCalledWith(items[1]);
    expect(document.activeElement).toBe(scroller);
  });
});
