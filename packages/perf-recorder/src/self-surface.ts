import { RECORDER_HOST_ID } from "./constants";

/**
 * True when an event originates from the recorder's own panel (which lives in a
 * Shadow root whose host carries RECORDER_HOST_ID), so the panel's own controls
 * are never logged as app interactions (MUL-4466 §10.2).
 *
 * Real browsers expose the host via `composedPath()` (and retarget `target` to
 * the host for outer-tree listeners) — that is the primary check. As a fallback
 * we walk the target's ancestor chain, hopping across shadow roots via `.host`
 * (checking the property rather than `instanceof ShadowRoot`, which jsdom does
 * not reliably satisfy).
 */
export function isRecorderEvent(event: Event): boolean {
  const path = typeof event.composedPath === "function" ? event.composedPath() : [];
  for (const node of path) {
    if (node instanceof Element && node.id === RECORDER_HOST_ID) return true;
  }
  let node: Node | null = event.target as Node | null;
  for (let hops = 0; node && hops < 200; hops++) {
    if (node instanceof Element && node.id === RECORDER_HOST_ID) return true;
    const host = (node as { host?: unknown }).host;
    node = host instanceof Element ? host : node.parentNode;
  }
  return false;
}
