import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ThreadMinimap, commentPreview, waveScale } from "./thread-minimap";

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) => `${type}:${id}`,
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
    { id: "c1", entry: comment("c1", "First thread opener\nwith details") },
    { id: "c2", entry: comment("c2", "Second thread opener") },
    { id: "c3", entry: comment("c3", "") },
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

  it("opens the shared preview card after the intent delay and closes after the leave grace", () => {
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
      expect(screen.queryByText("with details")).not.toBeInTheDocument();

      act(() => vi.advanceTimersByTime(150)); // intent delay elapses → card opens
      expect(screen.getByText("with details")).toBeInTheDocument();

      fireEvent.pointerLeave(nav);
      act(() => vi.advanceTimersByTime(30)); // wave-clear frame
      expect(screen.getByText("with details")).toBeInTheDocument(); // grace keeps it up
      act(() => vi.advanceTimersByTime(150)); // grace elapses → card closes
      expect(screen.queryByText("with details")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
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
