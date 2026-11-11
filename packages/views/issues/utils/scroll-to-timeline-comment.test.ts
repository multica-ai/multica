import { afterEach, describe, expect, it, vi } from "vitest";
import { scrollToTimelineComment } from "./scroll-to-timeline-comment";

const frame = () => new Promise((resolve) => requestAnimationFrame(resolve));

function anchor(container: HTMLElement, top: number) {
  const el = document.createElement("div");
  el.id = "comment-target";
  // Positioned like a real row: its viewport top moves with the scroll.
  el.getBoundingClientRect = () => el.isConnected
    ? new DOMRect(0, top - container.scrollTop, 400, 100)
    : new DOMRect(0, 0, 0, 0);
  return el;
}

describe("scrollToTimelineComment", () => {
  afterEach(() => { document.body.innerHTML = ""; });

  it("follows an anchor that a commit replaces between frames instead of scrolling to the top", async () => {
    const container = document.createElement("div");
    container.getBoundingClientRect = () => new DOMRect(0, 0, 400, 600);
    Object.defineProperty(container, "clientHeight", { value: 600 });
    document.body.append(container);
    container.append(anchor(container, 5000));
    const onComplete = vi.fn();
    const onMounted = vi.fn(() => {
      // The same commit that mounted the row re-renders it before the
      // alignment frame: the node the landing found is gone.
      container.replaceChildren(anchor(container, 5000));
    });

    scrollToTimelineComment({ container, virtualizer: null, index: 0, commentId: "target", onMounted, onComplete });
    for (let i = 0; i < 10 && !onComplete.mock.calls.length; i++) await frame();

    expect(onMounted).toHaveBeenCalledTimes(1);
    expect(onComplete).toHaveBeenCalled();
    // 5000 - (600 - 100) / 2: the replacement row, centred.
    expect(container.scrollTop).toBe(4750);
  });
});
