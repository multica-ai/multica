// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  createBottomStick,
  distanceFromBottom,
  STICK_EDGE_THRESHOLD,
  type ScrollMetrics,
} from "./stick-to-bottom";

const VIEWPORT = 600;

/** Metrics for a list of `content` px scrolled so `fromBottom` px remain below. */
function at(content: number, fromBottom: number): ScrollMetrics {
  return {
    clientHeight: VIEWPORT,
    scrollHeight: content,
    scrollTop: Math.max(0, content - VIEWPORT - fromBottom),
  };
}

describe("distanceFromBottom", () => {
  it("measures the content left below the fold", () => {
    expect(distanceFromBottom(at(2000, 340))).toBe(340);
  });

  it("floors at zero when the content is shorter than the viewport", () => {
    expect(distanceFromBottom({ clientHeight: 600, scrollHeight: 200, scrollTop: 0 })).toBe(0);
  });
});

describe("createBottomStick", () => {
  it("opens pinned so a freshly mounted session lands on the newest message", () => {
    expect(createBottomStick().isPinned()).toBe(true);
  });

  // The regression: a streaming reply is ONE Virtuoso row that keeps growing,
  // so the item count never changes and `followOutput` never fires. Every
  // growth tick has to re-pin on its own or the tail runs off the fold.
  it("follows a single row that grows across many streamed chunks", () => {
    const stick = createBottomStick();
    let content = 800;

    for (let chunk = 0; chunk < 40; chunk++) {
      content += 120;
      const target = stick.onResize({
        clientHeight: VIEWPORT,
        scrollHeight: content,
        scrollTop: 200,
      });
      expect(target).toBe(content - VIEWPORT);
    }
  });

  it("keeps following after each pin, so growth never accumulates below the fold", () => {
    const stick = createBottomStick();
    let scrollTop = 200;
    let content = 800;

    for (let chunk = 0; chunk < 10; chunk++) {
      content += 500;
      scrollTop = stick.onResize({ clientHeight: VIEWPORT, scrollHeight: content, scrollTop })!;
      // The browser fires a scroll event for the pin we just applied.
      stick.onScroll({ clientHeight: VIEWPORT, scrollHeight: content, scrollTop });
    }

    expect(stick.isPinned()).toBe(true);
    expect(distanceFromBottom({ clientHeight: VIEWPORT, scrollHeight: content, scrollTop })).toBe(0);
  });

  // A long reply that lands in one paint (cached session, non-streamed render)
  // is the same event as a stream: content far taller than the viewport, pinned
  // in a single step rather than a partial scroll.
  it("pins past a reply taller than the viewport in one step", () => {
    const stick = createBottomStick();
    expect(
      stick.onResize({ clientHeight: VIEWPORT, scrollHeight: 50_000, scrollTop: 0 }),
    ).toBe(50_000 - VIEWPORT);
  });

  // The composer grows with the draft (and banners/queue appear above it), which
  // shrinks the list's clientHeight while scrollTop stays put — the tail slides
  // out of view behind the composer.
  it("re-pins when the composer grows and shrinks the viewport", () => {
    const stick = createBottomStick();
    const content = 2000;
    stick.onScroll({ clientHeight: VIEWPORT, scrollHeight: content, scrollTop: content - VIEWPORT });

    // Composer grows by 3 lines (~72px): the same scrollTop now hides 72px.
    const shrunk = { clientHeight: VIEWPORT - 72, scrollHeight: content, scrollTop: content - VIEWPORT };
    expect(distanceFromBottom(shrunk)).toBe(72);
    expect(stick.onResize(shrunk)).toBe(content - (VIEWPORT - 72));
  });

  it("does not fight a reader who scrolled up to read history", () => {
    const stick = createBottomStick();
    stick.onScroll(at(4000, 900));

    expect(stick.isPinned()).toBe(false);
    expect(stick.onResize({ clientHeight: VIEWPORT, scrollHeight: 6000, scrollTop: 2500 })).toBeNull();
  });

  it("keeps following inside the edge threshold and releases past it", () => {
    const stick = createBottomStick();

    stick.onScroll(at(4000, STICK_EDGE_THRESHOLD));
    expect(stick.isPinned()).toBe(true);

    stick.onScroll(at(4000, STICK_EDGE_THRESHOLD + 1));
    expect(stick.isPinned()).toBe(false);
  });

  it("re-engages when the reader scrolls back down to the live end", () => {
    const stick = createBottomStick();
    stick.onScroll(at(4000, 900));
    stick.onScroll(at(4000, 0));

    expect(stick.isPinned()).toBe(true);
    expect(stick.onResize({ clientHeight: VIEWPORT, scrollHeight: 5000, scrollTop: 3400 })).toBe(4400);
  });

  it("never pins upward when content shrinks under a pinned viewport", () => {
    const stick = createBottomStick();
    // A collapsible closing: the browser clamps scrollTop itself, and dragging
    // the reader further up would be a jump they did not ask for.
    expect(
      stick.onResize({ clientHeight: VIEWPORT, scrollHeight: 1000, scrollTop: 900 }),
    ).toBeNull();
  });

  it("reports no work when a resize leaves the viewport already at the bottom", () => {
    const stick = createBottomStick();
    expect(
      stick.onResize({ clientHeight: VIEWPORT, scrollHeight: 2000, scrollTop: 1400 }),
    ).toBeNull();
  });
});
