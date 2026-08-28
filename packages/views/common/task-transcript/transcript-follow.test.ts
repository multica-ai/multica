// @vitest-environment node
import { describe, it, expect } from "vitest";
import { createLiveEndFollow, FOLLOW_EDGE_THRESHOLD } from "./transcript-follow";

function makeFollow(startAt = 0) {
  let t = startAt;
  const follow = createLiveEndFollow(() => t);
  const tick = (ms: number) => {
    t += ms;
  };
  follow.setActive(true);
  return { follow, tick };
}

describe("createLiveEndFollow", () => {
  it("starts following and pins system displacement back to the live end", () => {
    const { follow, tick } = makeFollow();
    tick(1000);
    // A prepend-anchoring shift: no user input at all.
    expect(follow.onScroll(500)).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("a system shift inside the reader's gesture is pinned, not blamed on them", () => {
    // Regression: absolute-position heuristics dropped the latch here — the
    // user nudged 30px (still inside the zone), then a flush pushed the
    // viewport past the threshold while their input was still fresh. Only
    // 30px of the movement is attributable to the reader; the rest is the
    // system's and is corrected immediately.
    const { follow, tick } = makeFollow();
    tick(1000);
    follow.input(30);
    tick(200); // inside the intent window
    expect(follow.onScroll(500)).toBe(true); // pin the system's 470px back
    expect(follow.isFollowing()).toBe(true);
  });

  it("releases when touch momentum carries a confirmed flick past the threshold", () => {
    const { follow } = makeFollow();
    follow.touchStart();
    follow.input(80);
    expect(follow.onScroll(80)).toBe(false);
    follow.touchEnd();

    expect(follow.onScroll(160)).toBe(false);
    expect(follow.isFollowing()).toBe(false);
  });

  it("counts the surface's fractional touch displacement", () => {
    const { follow } = makeFollow();
    follow.touchStart();
    follow.input(80);
    expect(follow.onScroll(80.5)).toBe(false);
    expect(follow.isFollowing()).toBe(true);
    follow.touchEnd();

    expect(follow.onScroll(120.25)).toBe(false);
    expect(follow.isFollowing()).toBe(false);
  });

  it("releases for wheel notches spaced past the intent window", () => {
    // A discrete mouse wheel at reading pace: one notch per event, far more
    // than the window apart. Attributed displacement accumulates across
    // gestures, so pacing and device units cannot defeat the threshold.
    const { follow, tick } = makeFollow();
    follow.input(100);
    expect(follow.onScroll(100)).toBe(false); // notch 1: 100px taken
    tick(500);
    follow.input(100);
    expect(follow.onScroll(200)).toBe(false); // notch 2: 200px taken, released
    expect(follow.isFollowing()).toBe(false);
  });

  it("accumulates displacement across pins during a fast stream", () => {
    // Sub-threshold notches each answered by a chunk that pins the reader
    // back: the taken displacement survives the pins, so persistence wins
    // instead of losing every round.
    const { follow, tick } = makeFollow();
    follow.input(60);
    expect(follow.onScroll(60)).toBe(false);
    tick(400);
    expect(follow.onResize(240)).toBe(true); // chunk pins back
    follow.input(60);
    expect(follow.onScroll(60)).toBe(false);
    tick(400);
    expect(follow.onResize(240)).toBe(true); // chunk pins back again
    follow.input(60);
    follow.onScroll(60); // 180px taken in total
    expect(follow.isFollowing()).toBe(false);
  });

  it("pins displacement that lands inside the intent window", () => {
    // The final chunk of a reply has a whole window to land in after any
    // small nudge; the pin decision must not hide behind a timer, because
    // no later event exists to re-evaluate it.
    const { follow } = makeFollow();
    follow.input(2);
    expect(follow.onScroll(2)).toBe(false); // 2px nudge, confirmed
    expect(follow.onResize(182)).toBe(true); // still inside the window: pin now
    expect(follow.isFollowing()).toBe(true);
  });

  it("re-engages a reader who walks themselves back to the live end", () => {
    const { follow } = makeFollow();
    follow.input(200);
    follow.onScroll(200); // released
    expect(follow.isFollowing()).toBe(false);
    follow.input(-200);
    expect(follow.onScroll(0)).toBe(false);
    expect(follow.isFollowing()).toBe(true);
  });

  it("disengages on accumulated user input once the surface's scroll confirms it", () => {
    const { follow } = makeFollow();
    follow.input(80);
    follow.input(80); // cumulative 160 > threshold — but only staged so far
    expect(follow.isFollowing()).toBe(true);
    expect(follow.onScroll(160)).toBe(false); // the surface moved: released, no pin
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(400)).toBe(false); // no pinning once disengaged
  });

  it("input the surface never consumed does not release, and never blocks the pin", () => {
    // A wheel over a nested scroller (or a list too short to scroll) bubbles
    // to the container without moving it: no scroll ever attributes it.
    const { follow } = makeFollow();
    follow.input(300);
    expect(follow.isFollowing()).toBe(true);
    expect(follow.onResize(180)).toBe(true); // growth pins straight away
    expect(follow.isFollowing()).toBe(true);
  });

  it("pins a system shift after the surface left input unconsumed", () => {
    const { follow, tick } = makeFollow();
    follow.input(300);
    follow.endInputFrame();
    tick(100);

    expect(follow.onScroll(200)).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("a new gesture does not inherit unconsumed intent from an old one", () => {
    const { follow, tick } = makeFollow();
    follow.input(300); // never confirmed by a scroll
    tick(1000);
    follow.input(60); // fresh gesture, sub-threshold on its own
    expect(follow.onScroll(60)).toBe(false);
    expect(follow.isFollowing()).toBe(true);
  });

  it("a mixed-direction gesture counts its net movement, not its pushes", () => {
    const { follow } = makeFollow();
    follow.input(100);
    follow.input(-90);
    follow.input(100);
    // The surface only net-moved 110: attribution is capped by movement.
    expect(follow.onScroll(110)).toBe(false);
    expect(follow.isFollowing()).toBe(true);
  });

  it("a stale gesture's budget does not attribute a later system shift", () => {
    const { follow, tick } = makeFollow();
    follow.input(100); // never confirmed
    tick(1000);
    expect(follow.onScroll(300)).toBe(true); // whole shift is the system's: pin
    follow.input(100); // fresh gesture: 100 <= threshold on its own
    expect(follow.isFollowing()).toBe(true);
  });

  it("re-engages when the viewport returns to the live-end zone", () => {
    const { follow } = makeFollow();
    follow.input(FOLLOW_EDGE_THRESHOLD + 1);
    follow.onScroll(FOLLOW_EDGE_THRESHOLD + 1);
    expect(follow.isFollowing()).toBe(false);
    follow.onAtEdgeChange(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("scrollbar drag past the threshold disengages", () => {
    const { follow } = makeFollow();
    follow.pointerDown(true);
    expect(follow.onScroll(80)).toBe(false); // still in zone
    expect(follow.isFollowing()).toBe(true);
    follow.onScroll(200);
    expect(follow.isFollowing()).toBe(false);
    follow.pointerUp();
  });

  it("never pins while the mouse is held on row content (text selection autoscroll)", () => {
    // Regression: selection-drag autoscroll has no wheel/touch input; pinning
    // during it made text below the fold unselectable.
    const { follow, tick } = makeFollow();
    tick(1000);
    follow.pointerDown(false); // mousedown on a row, not the scrollbar
    expect(follow.onScroll(600)).toBe(false);
    expect(follow.isFollowing()).toBe(true); // content drag is not scroll intent
    follow.pointerUp();
    expect(follow.onScroll(600)).toBe(true); // released: enforcement resumes
  });

  it("explicit disengage (segment navigation) stops the pinning", () => {
    const { follow, tick } = makeFollow();
    follow.disengage();
    tick(1000);
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(300)).toBe(false);
  });

  it("reset re-engages and clears held-pointer state", () => {
    const { follow, tick } = makeFollow();
    follow.pointerDown(false);
    follow.input(500);
    follow.reset();
    tick(1000);
    expect(follow.isFollowing()).toBe(true);
    expect(follow.onScroll(50)).toBe(true); // mouseHeld cleared by reset
  });

  it("is fully inert when inactive (chronological or completed task)", () => {
    const { follow, tick } = makeFollow();
    follow.setActive(false);
    tick(1000);
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(500)).toBe(false);
    follow.input(500); // ignored
    follow.setActive(true);
    expect(follow.isFollowing()).toBe(true); // latch untouched while inactive
  });
});
