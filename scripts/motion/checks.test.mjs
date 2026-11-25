import assert from "node:assert/strict";
import test from "node:test";

import { animated, describe, instant, landsOn, monotonic, track } from "./checks.mjs";

const frames = (heights) => heights.map((h, i) => ({ t: i * 16.7, rects: { list: { x: 0, y: 100, w: 240, h } } }));

test("track reads derived edges and skips frames without the element", () => {
  const sampled = [
    { t: 0, rects: { pill: { x: 10, y: 0, w: 40, h: 20 } } },
    { t: 16, rects: { pill: null } },
    { t: 33, rects: { pill: { x: 30, y: 0, w: 60, h: 20 } } },
  ];
  assert.deepEqual(track(sampled, "pill", "right"), [50, 90]);
  assert.deepEqual(track(sampled, "pill", "x"), [10, 30]);
});

test("a height easing from one size to the other is monotonic and animated", () => {
  const values = track(frames([318, 318, 210, 140, 98, 74, 63, 60, 60]), "list", "h");
  assert.equal(monotonic(values).ok, true);
  assert.equal(animated(values).ok, true);
  assert.equal(instant(values).ok, false);
  assert.equal(describe(values), "318 → 60 over 6 frames");
});

test("a block that jumps up while the next tab loads and is pushed back down is not monotonic", () => {
  const result = monotonic([500, 500, 468, 468, 468, 500, 500]);
  assert.equal(result.ok, false);
  assert.match(result.detail, /moved 32px and came back/);
});

test("an indicator that overshoots its target reverses", () => {
  const result = monotonic([0, 40, 90, 117, 104, 100, 100]);
  assert.equal(result.ok, false);
  assert.match(result.detail, /reversed by 17px/);
});

test("sub-pixel jitter stays within the overshoot tolerance", () => {
  assert.equal(monotonic([10, 20, 30, 29.7, 30]).ok, true);
});

test("an ease-out tail moving under half a pixel per frame still counts as motion", () => {
  const tail = [70, 92, 100, 103.5, 104.9, 105.4, 105.7, 105.8];
  assert.equal(animated(tail).ok, true);
  assert.equal(instant(tail).ok, false);
});

test("reduced motion: each change lands in one frame, even when it changes twice", () => {
  assert.equal(instant([318, 318, 0, 0, 0, 60, 60]).ok, true);
  assert.equal(instant([318, 200, 120, 60]).ok, false);
  assert.equal(animated([318, 318, 60, 60]).ok, false);
});

test("landsOn compares the last frame along the requested edges", () => {
  const sampled = [
    { t: 0, rects: { pill: { x: 0, y: 0, w: 40, h: 20 }, tab: { x: 60, y: 0, w: 72, h: 20 } } },
    { t: 16, rects: { pill: { x: 60.2, y: 0, w: 72, h: 20 }, tab: { x: 60, y: 0, w: 72, h: 20 } } },
  ];
  assert.equal(landsOn(sampled, "pill", "tab").ok, true);
  const off = landsOn([{ t: 0, rects: { pill: { x: 60, y: 0, w: 139, h: 20 }, tab: { x: 60, y: 0, w: 72, h: 20 } } }], "pill", "tab");
  assert.equal(off.ok, false);
  assert.equal(off.detail, "x +0px, w +67px");
});

test("a missing element fails instead of passing vacuously", () => {
  assert.equal(monotonic([]).ok, false);
  assert.equal(instant([]).ok, false);
  assert.equal(landsOn([{ t: 0, rects: { pill: null, tab: { x: 0, y: 0, w: 1, h: 1 } } }], "pill", "tab").ok, false);
});
