import { forwardRef, useImperativeHandle } from "react";
import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxList } from "./inbox-list";

const scrollToIndex = vi.fn();

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    props: {
      data: InboxItem[];
      itemContent: (index: number, item: InboxItem) => React.ReactNode;
    },
    ref,
  ) {
    useImperativeHandle(ref, () => ({ scrollToIndex }));
    return (
      <div>
        {props.data.map((item, index) => (
          <div key={item.id}>{props.itemContent(index, item)}</div>
        ))}
      </div>
    );
  }),
}));

vi.mock("./inbox-list-item", () => ({
  InboxListItem: ({ item }: { item: InboxItem }) => <div>{item.id}</div>,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "Inbox" }),
}));

function item(id: string): InboxItem {
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
    title: id,
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
  };
}

describe("InboxList unread scrolling", () => {
  beforeEach(() => {
    scrollToIndex.mockReset();
  });

  it("smoothly aligns the requested unread item to the top", async () => {
    const { container, rerender } = render(
      <InboxList
        items={[item("first"), item("target"), item("last")]}
        view="inbox"
        selectedKey=""
        archivedCount={0}
        onSelect={vi.fn()}
        onAction={vi.fn()}
        onOpenArchived={vi.fn()}
        scrollRequest={null}
      />,
    );
    const scrollContainer = container.firstElementChild as HTMLDivElement;
    Object.defineProperties(scrollContainer, {
      clientHeight: { configurable: true, value: 500 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    rerender(
      <InboxList
        items={[item("first"), item("target"), item("last")]}
        view="inbox"
        selectedKey=""
        archivedCount={0}
        onSelect={vi.fn()}
        onAction={vi.fn()}
        onOpenArchived={vi.fn()}
        scrollRequest={{ itemId: "target", sequence: 1 }}
      />,
    );

    await waitFor(() =>
      expect(scrollToIndex).toHaveBeenCalledWith({
        index: 1,
        align: "start",
        behavior: "smooth",
      }),
    );
  });

  it("does not reposition the item when the list fits in one viewport", async () => {
    const { container, rerender } = render(
      <InboxList
        items={[item("first"), item("target")]}
        view="inbox"
        selectedKey=""
        archivedCount={0}
        onSelect={vi.fn()}
        onAction={vi.fn()}
        onOpenArchived={vi.fn()}
        scrollRequest={null}
      />,
    );
    const scrollContainer = container.firstElementChild as HTMLDivElement;
    Object.defineProperties(scrollContainer, {
      clientHeight: { configurable: true, value: 500 },
      scrollHeight: { configurable: true, value: 500 },
    });

    rerender(
      <InboxList
        items={[item("first"), item("target")]}
        view="inbox"
        selectedKey=""
        archivedCount={0}
        onSelect={vi.fn()}
        onAction={vi.fn()}
        onOpenArchived={vi.fn()}
        scrollRequest={{ itemId: "target", sequence: 1 }}
      />,
    );

    await waitFor(() => expect(scrollToIndex).not.toHaveBeenCalled());
  });
});
