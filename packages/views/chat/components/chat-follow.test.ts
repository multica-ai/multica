// @vitest-environment node
import { describe, expect, it } from "vitest";
import { createChatFollow, type ScrollMetrics } from "./chat-follow";

function setup() {
  let time = 0;
  const follow = createChatFollow(() => time);
  const metrics: ScrollMetrics = { scrollTop: 1400, scrollHeight: 2000, clientHeight: 600 };
  follow.syncPosition(metrics.scrollTop);
  return { follow, metrics, tick: (ms: number) => { time += ms; } };
}

describe("chat follow intent", () => {
  it.each([0.5, 1, 1.25, 2, 100.5])("accepts list displacement %s without matching an input budget", (px) => {
    const { follow, metrics, tick } = setup();
    follow.input(-0.5);
    tick(32); // The confirming scroll can arrive after a rendering frame.
    metrics.scrollTop -= px;
    expect(follow.onScroll(metrics).pin).toBe(false);
    expect(follow.isFollowing()).toBe(false);
  });

  it("separates row growth and an unlanded pin from reader scrolling", () => {
    const { follow, metrics } = setup();
    metrics.scrollHeight += 21;
    expect(follow.onResize(metrics)).toBe(true);
    follow.input(-40);
    metrics.scrollTop -= 40; // The requested pin has not landed.
    expect(follow.onScroll(metrics).pin).toBe(false);
    expect(follow.isFollowing()).toBe(false);
  });

  it("pins the final content growth even with an unused candidate input", () => {
    const { follow, metrics } = setup();
    follow.input(-1);
    metrics.scrollHeight += 21;
    expect(follow.onScroll(metrics).pin).toBe(true);
    expect(follow.onResize(metrics)).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("does not release for a clamp at the live end", () => {
    const { follow, metrics } = setup();
    follow.input(-1);
    metrics.clientHeight += 72;
    metrics.scrollTop -= 72;
    expect(follow.onScroll(metrics).pin).toBe(false);
    expect(follow.isFollowing()).toBe(true);
  });

  it("does not attribute a later system scroll to expired input", () => {
    const { follow, metrics, tick } = setup();
    follow.input(-1);
    tick(301);
    metrics.scrollTop -= 60;
    expect(follow.onScroll(metrics).pin).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("keeps confirming same-direction momentum while the list is moving", () => {
    const { follow, metrics, tick } = setup();
    follow.input(-1);
    for (let i = 0; i < 4; i++) {
      tick(200);
      metrics.scrollTop -= 20;
      expect(follow.onScroll(metrics).readerMoved).toBe(true);
    }
    expect(follow.isFollowing()).toBe(false);
  });

  it("rechecks whether a control prevented the input's default action", () => {
    const { follow, metrics } = setup();
    let prevented = false;
    follow.input(-1, () => !prevented);
    prevented = true;
    metrics.scrollTop -= 60;
    expect(follow.onScroll(metrics).pin).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("ignores system returns and resumes on a reader's downward return", () => {
    const { follow, metrics } = setup();
    follow.input(-1);
    metrics.scrollTop -= 100;
    follow.onScroll(metrics);
    metrics.scrollTop = 1400; // An old programmatic pin cannot resume follow.
    follow.onScroll(metrics);
    expect(follow.isFollowing()).toBe(false);
    follow.input(-1);
    metrics.scrollTop -= 100;
    follow.onScroll(metrics);
    follow.input(1);
    metrics.scrollTop = 1397;
    follow.onScroll(metrics);
    expect(follow.isFollowing()).toBe(true);
  });

  it("leaves scrollbar drags alone and resumes when dragged back", () => {
    const { follow, metrics } = setup();
    follow.pointerDown(true);
    metrics.scrollTop -= 100;
    expect(follow.onScroll(metrics).pin).toBe(false);
    expect(follow.isFollowing()).toBe(false);
    metrics.scrollTop += 100;
    follow.onScroll(metrics);
    expect(follow.isFollowing()).toBe(true);
    follow.pointerUp();
  });

  it("suppresses pins during text selection without calling it scroll intent", () => {
    const { follow, metrics } = setup();
    follow.pointerDown(false);
    metrics.scrollTop -= 100;
    expect(follow.onScroll(metrics).pin).toBe(false);
    expect(follow.isFollowing()).toBe(true);
    follow.pointerUp();
    expect(follow.onResize(metrics)).toBe(true);
  });
});
