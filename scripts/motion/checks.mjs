// Checks over the frames `scripts/motion/capture.mjs` samples: one entry per
// animation frame, `{ t, rects: { name: { x, y, w, h } | null } }`, in CSS
// pixels relative to the viewport. Each check returns `{ ok, detail }`.

const DEFAULT_TOLERANCE = 0.5;
// A still element reads back the same float every frame, and an ease-out tail
// at 120Hz moves well under the tolerance per frame, so any difference counts.
const CHANGED = 0.01;

function value(rect, prop) {
  if (!rect) return null;
  if (prop === "right") return rect.x + rect.w;
  if (prop === "bottom") return rect.y + rect.h;
  return rect[prop];
}

/** One property of one sampled element, frame by frame; frames where it is absent are dropped. */
export function track(frames, name, prop) {
  return frames.map((frame) => value(frame.rects[name], prop)).filter((v) => v !== null && v !== undefined);
}

const round = (v) => Math.round(v * 10) / 10;

/** How many frames the value changed in, and the longest run of consecutive changing frames. */
function movement(values) {
  let frames = 0;
  let run = 0;
  let longestRun = 0;
  for (let i = 1; i < values.length; i++) {
    if (Math.abs(values[i] - values[i - 1]) > CHANGED) {
      frames++;
      run++;
      longestRun = Math.max(longestRun, run);
    } else {
      run = 0;
    }
  }
  return { frames, longestRun };
}

/** "318 → 60 over 12 frames": what a track did, for a report line. */
export function describe(values) {
  if (values.length === 0) return "not found";
  const { frames } = movement(values);
  const first = round(values[0]);
  const last = round(values.at(-1));
  if (frames === 0) return `${first}, still`;
  return `${first} → ${last} over ${frames} frame${frames === 1 ? "" : "s"}`;
}

/**
 * The value moves in one direction only: a height that eases from the old
 * size to the new, an indicator that does not overshoot its target, a block
 * below that does not jump away and come back. A track that ends where it
 * started must not move at all.
 */
export function monotonic(values, { tolerance = DEFAULT_TOLERANCE } = {}) {
  if (values.length === 0) return { ok: false, detail: "not found" };
  const first = values[0];
  const last = values.at(-1);
  const direction = Math.abs(last - first) <= tolerance ? 0 : Math.sign(last - first);
  let worst = 0;
  let extreme = first;
  for (const v of values) {
    if (direction === 0) {
      worst = Math.max(worst, Math.abs(v - first));
    } else {
      extreme = direction > 0 ? Math.max(extreme, v) : Math.min(extreme, v);
      worst = Math.max(worst, Math.abs(v - extreme));
    }
  }
  const ok = worst <= tolerance;
  const what = direction === 0 ? `moved ${round(worst)}px and came back` : `reversed by ${round(worst)}px`;
  return { ok, detail: ok ? describe(values) : `${what} (${describe(values)})` };
}

/** The change is spread over several frames rather than applied in one. */
export function animated(values, { minFrames = 3 } = {}) {
  if (values.length === 0) return { ok: false, detail: "not found" };
  const { frames } = movement(values);
  return { ok: frames >= minFrames, detail: `${describe(values)}; expected at least ${minFrames}` };
}

/**
 * Every change lands in a single frame, as reduced motion requires. A track
 * may change more than once (an empty list while the next tab loads, then its
 * rows), but never in two frames in a row.
 */
export function instant(values) {
  if (values.length === 0) return { ok: false, detail: "not found" };
  const { longestRun } = movement(values);
  const ok = longestRun <= 1;
  return { ok, detail: ok ? describe(values) : `moved for ${longestRun} frames in a row (${describe(values)})` };
}

/** In the last frame, element `a` sits exactly on element `b` along `props`. */
export function landsOn(frames, a, b, { props = ["x", "w"], tolerance = DEFAULT_TOLERANCE } = {}) {
  const last = frames.at(-1)?.rects ?? {};
  if (!last[a] || !last[b]) return { ok: false, detail: `${last[a] ? b : a} not found in the last frame` };
  const off = props.map((prop) => [prop, value(last[a], prop) - value(last[b], prop)]);
  const ok = off.every(([, d]) => Math.abs(d) <= tolerance);
  return { ok, detail: off.map(([prop, d]) => `${prop} ${d >= 0 ? "+" : ""}${round(d)}px`).join(", ") };
}
