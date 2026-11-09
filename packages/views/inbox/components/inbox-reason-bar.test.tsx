import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import en from "../../locales/en/inbox.json";
import { InboxReasonBar } from "./inbox-reason-bar";

vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/shortcut-keycaps", () => ({ ActionShortcutKeycaps: () => null }));
vi.mock("./inbox-detail-label", () => ({ InboxDetailLabel: () => <>mentioned you</> }));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => () => "2m" }));
vi.mock("../../i18n", () => ({
  useT: () => ({ t: (select: (messages: typeof en) => string) => select(en) }),
}));

const item = { id: "inbox-a", issue_id: "issue-a", created_at: "2026-01-01T00:00:00Z" } as InboxItem;

describe("InboxReasonBar", () => {
  it("says why the issue is waiting until the user acts", () => {
    const onArchive = vi.fn();
    render(<InboxReasonBar item={item} handled={false} onArchive={onArchive} />);

    expect(screen.getByText("mentioned you")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: en.detail.archive }));
    expect(onArchive).toHaveBeenCalled();
  });

  it("swaps the reason for what the user did, keeping Archive", () => {
    const { rerender } = render(
      <InboxReasonBar item={item} handled={false} acted="replied" onArchive={vi.fn()} />,
    );
    expect(screen.queryByText("mentioned you")).toBeNull();
    expect(screen.getByText(en.detail.acted_replied)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: en.detail.archive })).toBeInTheDocument();

    rerender(<InboxReasonBar item={item} handled={false} acted="updated" onArchive={vi.fn()} />);
    expect(screen.getByText(en.detail.acted_updated)).toBeInTheDocument();
  });

  it("drops Archive once the issue has left the Inbox", () => {
    render(<InboxReasonBar item={item} handled acted="replied" onArchive={vi.fn()} />);
    expect(screen.getByText(en.detail.handled)).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
