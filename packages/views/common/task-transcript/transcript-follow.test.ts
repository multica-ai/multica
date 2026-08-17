import { describe, it, expect } from "vitest";
import { createNewestFirstFollow, FOLLOW_EDGE_THRESHOLD } from "./transcript-follow";

function makeFollow(startAt = 0) {
  let t = startAt;
  const follow = createNewestFirstFollow(() => t);
  const tick = (ms: number) => {
    t += ms;
  };
  follow.setActive(true);
  return { follow, tick };
}

describe("createNewestFirstFollow", () => {
  it("starts following and pins system displacement back to the live end", () => {
    const { follow, tick } = makeFollow();
    tick(1000);
    // A prepend-anchoring shift: no user input at all.
    expect(follow.onScroll(500)).toBe(true);
    expect(follow.isFollowing()).toBe(true);
  });

  it("does not disengage when a prepend shift lands inside the input window after an in-zone nudge", () => {
    // Regression: absolute-position heuristics dropped the latch here — the
    // user nudged 30px (still inside the zone), then a flush pushed the
    // viewport past the threshold while their input was still fresh.
    const { follow, tick } = makeFollow();
    tick(1000);
    follow.input(30);
    tick(200); // inside the intent window
    expect(follow.onScroll(500)).toBe(false); // user mid-gesture: no pin either
    expect(follow.isFollowing()).toBe(true);
    tick(400); // gesture over
    expect(follow.onScroll(500)).toBe(true); // now pin back
    expect(follow.isFollowing()).toBe(true);
  });

  it("disengages on accumulated user input beyond the threshold", () => {
    const { follow } = makeFollow();
    follow.input(80);
    expect(follow.isFollowing()).toBe(true);
    follow.input(80); // cumulative 160 > threshold
    expect(follow.isFollowing()).toBe(false);
    expect(follow.onScroll(400)).toBe(false); // no pinning once disengaged
  });

  it("upward input rolls the accumulator back instead of counting as intent", () => {
    const { follow } = makeFollow();
    follow.input(100);
    follow.input(-90);
    follow.input(100); // net 110 <= threshold
    expect(follow.isFollowing()).toBe(true);
  });

  it("a pin clears sub-threshold residue so old nudges cannot accumulate", () => {
    const { follow, tick } = makeFollow();
    follow.input(100);
    tick(1000);
    expect(follow.onScroll(300)).toBe(true); // pinned; residue cleared
    follow.input(100); // fresh gesture: 100 <= threshold on its own
    expect(follow.isFollowing()).toBe(true);
  });

  it("re-engages when the viewport returns to the top zone", () => {
    const { follow } = makeFollow();
    follow.input(FOLLOW_EDGE_THRESHOLD + 1);
    expect(follow.isFollowing()).toBe(false);
    follow.onAtTopChange(true);
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
