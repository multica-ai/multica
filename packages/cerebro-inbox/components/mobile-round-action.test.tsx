// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MobileRowActions, roundRowAction } from "./cerebro-inbox-row-actions";

describe("roundRowAction", () => {
  it("turns the existing row menu action into a direct Remove from <Round> action", () => {
    const result = roundRowAction([
      {
        round: { id: "round-1", name: "Weekly commercial review" },
        members: [{ issue_id: "issue-1" }],
      },
    ] as never, "issue-1");

    expect(result).toEqual({
      label: "Remove from Weekly commercial review",
      roundId: "round-1",
    });
  });
});

describe("MobileRowActions round access", () => {
  it("reveals Add to Round in the mobile swipe actions and invokes it", () => {
    const onAddToRound = vi.fn();
    const { container } = render(
      <div className="relative">
        <MobileRowActions
          muted={false}
          unread
          onArchive={vi.fn()}
          onToggleRead={vi.fn()}
          onToggleMute={vi.fn()}
          onAddToRound={onAddToRound}
          renderDrawerMenu={() => null}
          strings={{
            swipe_archive: "Archive",
            mark_read: "Mark read",
            mark_unread: "Mark unread",
            mute: "Remind me",
            unmute: "Remove reminder",
            drawer_title: "Message actions",
          } as never}
        />
      </div>,
    );

    const touchSurface = container.querySelector<HTMLElement>("[style*='touch-action']");
    expect(touchSurface).not.toBeNull();
    fireEvent.touchStart(touchSurface!, { touches: [{ clientX: 240, clientY: 40 }] });
    fireEvent.touchMove(touchSurface!, { touches: [{ clientX: 20, clientY: 42 }] });
    fireEvent.touchEnd(touchSurface!, { changedTouches: [{ clientX: 20, clientY: 42 }] });

    fireEvent.click(screen.getByText("Round"));
    expect(onAddToRound).toHaveBeenCalledOnce();
  });
});
