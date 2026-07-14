/**
 * DOM id of the recorder's Shadow-root host element. Shared between the panel
 * (which owns the element) and the collectors (which must exclude the recorder's
 * own surface from what they observe — MUL-4466 §10.2). Kept in a dependency-free
 * module so importing it never creates a panel ⇆ recorder cycle.
 */
export const RECORDER_HOST_ID = "multica-perf-recorder";
