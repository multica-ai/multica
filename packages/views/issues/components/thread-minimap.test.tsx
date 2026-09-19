import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen, within } from "@testing-library/react";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ThreadMinimap, commentPreview, waveScale } from "./thread-minimap";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) => `${type}:${id}`,
    getActorInitials: (_type: string, _id: string, name: string) => name.slice(0, 1),
    getActorAvatarUrl: () => null,
  }),
}));

function comment(id: string, content: string): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: `author-${id}`,
    created_at: "2026-07-10T10:00:00Z",
    content,
  };
}

describe("commentPreview", () => {
  it("splits the first line into the title and joins the rest into the body", () => {
    const { title, body } = commentPreview(
      "## Rollout plan\n\nShip the flag first.\nThen watch the dashboards.",
    );
    expect(title).toBe("Rollout plan");
    expect(body).toBe("Ship the flag first. Then watch the dashboards.");
  });

  it("flattens markdown decorations to plain text", () => {
    const { title, body } = commentPreview(
      "**Bold** start with [a link](https://example.com) and [@Walt](mention://agent/a-1)\n" +
        "- first item\n" +
        "1. numbered ![diagram](https://example.com/x.png)\n" +
        "```ts\nconst hidden = true;\n```\n" +
        "> quoted tail",
    );
    expect(title).toBe("Bold start with a link and @Walt");
    expect(body).toBe("first item numbered diagram quoted tail");
  });

  it("returns empty strings for content that flattens to nothing", () => {
    expect(commentPreview("![](https://example.com/only-image.png)")).toEqual({
      title: "",
      body: "",
    });
  });

  it("caps runaway titles and bodies", () => {
    const { title, body } = commentPreview(`${"t".repeat(500)}\n${"b".repeat(900)}`);
    expect(title).toHaveLength(200);
    expect(body).toHaveLength(300);
  });
});

describe("waveScale", () => {
  it("peaks under the cursor and settles to 1 at the radius", () => {
    expect(waveScale(0)).toBeCloseTo(1.7, 5);
    expect(waveScale(56)).toBe(1);
    expect(waveScale(200)).toBe(1);
  });

  it("tapers monotonically and symmetrically", () => {
    const profile = [0, 14, 28, 42, 56].map(waveScale);
    for (let i = 1; i < profile.length; i++) {
      expect(profile[i]!).toBeLessThan(profile[i - 1]!);
    }
    expect(waveScale(-14)).toBeCloseTo(waveScale(14), 10);
  });
});

describe("ThreadMinimap", () => {
  const threads = [
    { id: "c1", entry: comment("c1", "First thread opener\nwith details"), resolved: false, participants: [] },
    { id: "c2", entry: comment("c2", "Second thread opener"), resolved: false, participants: [] },
    { id: "c3", entry: comment("c3", ""), resolved: false, participants: [] },
  ];

  it("renders nothing below the thread threshold", () => {
    const { container } = renderWithI18n(
      <ThreadMinimap threads={threads.slice(0, 1)} scrollContainerEl={null} onJump={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders one labelled tick per thread, falling back to the author for empty content", () => {
    renderWithI18n(
      <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={vi.fn()} />,
    );

    const nav = screen.getByRole("navigation", { name: "Jump to comment thread" });
    expect(nav).toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "First thread opener" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Second thread opener" })).toBeInTheDocument();
    // Attachment-only comment: accessible name falls back to the actor.
    expect(screen.getByRole("button", { name: "member:author-c3" })).toBeInTheDocument();
  });

  it("opens all thread titles after the intent delay and closes after the leave grace", () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame"],
    });
    try {
      renderWithI18n(
        <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={vi.fn()} />,
      );
      const nav = screen.getByRole("navigation", { name: "Jump to comment thread" });

      // jsdom rects are all zero → the nearest tick resolves to index 0.
      fireEvent.pointerMove(nav, { clientY: 0 });
      act(() => vi.advanceTimersByTime(30)); // rAF flush — arms the intent timer
      expect(screen.queryByRole("list")).not.toBeInTheDocument();

      act(() => vi.advanceTimersByTime(150)); // intent delay elapses → card opens
      const list = screen.getByRole("list");
      expect(within(list).getAllByRole("button")).toHaveLength(3);
      expect(within(list).getByText("First thread opener")).toBeInTheDocument();
      expect(within(list).getByText("Second thread opener")).toBeInTheDocument();
      expect(within(list).getByText("member:author-c3")).toBeInTheDocument();
      expect(screen.queryByText("with details")).not.toBeInTheDocument();

      fireEvent.pointerLeave(nav);
      act(() => vi.advanceTimersByTime(30)); // wave-clear frame
      expect(screen.getByRole("list")).toBeInTheDocument(); // grace keeps it up
      act(() => vi.advanceTimersByTime(150)); // grace elapses → card closes
      expect(screen.queryByRole("list")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("hugs the scrollbar side: ticks are right-aligned and the card opens inward", () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame"],
    });
    try {
      renderWithI18n(
        <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={vi.fn()} />,
      );

      // The rail sits in the right gutter, so ticks flush right and the wave
      // grows them inward (away from the scrollbar) rather than over it.
      const tick = screen.getByRole("button", { name: "First thread opener" });
      expect(tick).toHaveClass("justify-end");
      expect(tick.firstElementChild).toHaveClass("origin-right");

      const nav = screen.getByRole("navigation", { name: "Jump to comment thread" });
      fireEvent.pointerMove(nav, { clientY: 0 });
      act(() => vi.advanceTimersByTime(30 + 150)); // rAF flush + intent delay

      const card = screen.getByRole("list").closest("div");
      expect(card).toHaveClass("right-8");
      expect(card?.className).not.toMatch(/(?:^|\s)left-/);
    } finally {
      vi.useRealTimers();
    }
  });

  it("marks resolved threads in the complete outline", () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame"],
    });
    try {
      renderWithI18n(
        <ThreadMinimap
          threads={[{ ...threads[0]!, resolved: true }, threads[1]!, threads[2]!]}
          scrollContainerEl={null}
          onJump={vi.fn()}
        />,
      );
      const nav = screen.getByRole("navigation", { name: "Jump to comment thread" });

      // jsdom rects are all zero → the nearest tick resolves to index 0, the
      // resolved thread.
      fireEvent.pointerMove(nav, { clientY: 0 });
      act(() => vi.advanceTimersByTime(30 + 150)); // rAF flush + intent delay
      expect(screen.getByLabelText("Resolved")).toBeInTheDocument();

      // Closing the outline removes its resolution indicator too.
      fireEvent.pointerLeave(nav);
      act(() => vi.advanceTimersByTime(30 + 150));
      expect(screen.queryByLabelText("Resolved")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the outline open while moving onto it and jumps from any title", () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame"],
    });
    try {
      const onJump = vi.fn();
      renderWithI18n(
        <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={onJump} />,
      );
      const nav = screen.getByRole("navigation");
      fireEvent.pointerMove(nav, { clientY: 0 });
      act(() => vi.advanceTimersByTime(180));
      const list = screen.getByRole("list");
      fireEvent.pointerLeave(nav);
      fireEvent.pointerEnter(list.parentElement!);
      act(() => vi.advanceTimersByTime(180));
      expect(list).toBeInTheDocument();
      const second = within(list).getByRole("button", { name: "Second thread opener" });
      fireEvent.pointerEnter(second);
      expect(second).toHaveAttribute("data-active");
      fireEvent.click(second);
      expect(onJump).toHaveBeenCalledWith("c2");
    } finally {
      vi.useRealTimers();
    }
  });

  it.each(["rail", "outline"])("closes after clicking the %s and moving away", (target) => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout", "requestAnimationFrame", "cancelAnimationFrame"],
    });
    try {
      const onJump = vi.fn();
      renderWithI18n(
        <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={onJump} />,
      );
      const nav = screen.getByRole("navigation");
      fireEvent.pointerMove(nav, { clientY: 0 });
      act(() => vi.advanceTimersByTime(180));
      const card = screen.getByRole("list").parentElement!;
      const button = within(target === "rail" ? nav : card)
        .getByRole("button", { name: "Second thread opener" });
      if (target === "outline") {
        fireEvent.pointerLeave(nav);
        fireEvent.pointerEnter(card);
      }
      // Native mouse clicks focus buttons before dispatching click.
      act(() => button.focus());
      fireEvent.click(button, { detail: 1 });
      expect(onJump).toHaveBeenCalledWith("c2");
      act(() => vi.advanceTimersByTime(180));
      expect(screen.getByRole("list")).toBeInTheDocument();
      fireEvent.pointerLeave(target === "rail" ? nav : card);
      act(() => vi.advanceTimersByTime(180));
      expect(screen.queryByRole("list")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps keyboard focus after activating an outline title", () => {
    vi.useFakeTimers();
    try {
      const onJump = vi.fn();
      renderWithI18n(
        <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={onJump} />,
      );
      act(() => screen.getByRole("button", { name: "First thread opener" }).focus());
      const card = screen.getByRole("list").parentElement!;
      const button = within(card).getByRole("button", { name: "Second thread opener" });
      act(() => button.focus());
      fireEvent.click(button, { detail: 0 });
      fireEvent.pointerLeave(card);
      act(() => vi.advanceTimersByTime(180));
      expect(onJump).toHaveBeenCalledWith("c2");
      expect(button).toHaveFocus();
      expect(screen.getByRole("list")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("opens on keyboard focus and dismisses with Escape, returning focus to the matching tick", () => {
    renderWithI18n(
      <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={vi.fn()} />,
    );
    const nav = screen.getByRole("navigation");
    act(() => within(nav).getByRole("button", { name: "First thread opener" }).focus());
    const list = screen.getByRole("list");
    const second = within(list).getByRole("button", { name: "Second thread opener" });
    act(() => second.focus());
    fireEvent.keyDown(second, { key: "Escape" });
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
    expect(within(nav).getByRole("button", { name: "Second thread opener" })).toHaveFocus();
  });

  it("shows member and agent avatars with overflow alongside resolution", () => {
    const participants = [
      { ...comment("alice", ""), actor_name: "Alice", actor_avatar_url: "https://example.com/alice.png" },
      { ...comment("agent", ""), actor_type: "agent", actor_name: "Design agent" },
      { ...comment("bob", ""), actor_name: "Bob" },
      { ...comment("carol", ""), actor_name: "Carol" },
    ];
    renderWithI18n(
      <ThreadMinimap
        threads={[{ ...threads[0]!, resolved: true, participants }, ...threads.slice(1)]}
        scrollContainerEl={null}
        onJump={vi.fn()}
      />,
    );
    act(() => screen.getByRole("button", { name: "First thread opener (resolved)" }).focus());
    const row = within(screen.getByRole("list"))
      .getByRole("button", { name: "First thread opener (resolved)" });
    expect(row).toHaveAttribute("aria-description", "Alice, Design agent, Bob, Carol");
    expect(within(row).getByAltText("Alice")).toHaveAttribute("src", "https://example.com/alice.png");
    expect(within(row).getByTitle("Design agent").querySelector(".lucide-bot")).not.toBeNull();
    expect(within(row).getByTitle("Bob")).toBeInTheDocument();
    expect(within(row).getByTitle("Carol")).toHaveTextContent("+1");
    expect(within(row).getByLabelText("Resolved")).toBeInTheDocument();
    expect(within(row).queryByRole("link")).not.toBeInTheDocument();
  });

  it("carries the resolved state in the tick's accessible name", () => {
    renderWithI18n(
      <ThreadMinimap
        threads={[{ ...threads[0]!, resolved: true }, threads[1]!, threads[2]!]}
        scrollContainerEl={null}
        onJump={vi.fn()}
      />,
    );

    // The collapsed rail announces resolution before the outline is opened.
    expect(
      screen.getByRole("button", { name: "First thread opener (resolved)" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Second thread opener" })).toBeInTheDocument();
  });

  it("jumps to the clicked thread", () => {
    const onJump = vi.fn();
    renderWithI18n(
      <ThreadMinimap threads={threads} scrollContainerEl={null} onJump={onJump} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Second thread opener" }));
    expect(onJump).toHaveBeenCalledTimes(1);
    expect(onJump).toHaveBeenCalledWith("c2");
  });
});
