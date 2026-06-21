import type { TimelineGroup } from "./grouping";

// Approximate context-fill estimate for a chapter/session (FIR-1769 P3).
//
// This is intentionally a heuristic, mirroring cerebro-profile's char-based
// token estimate (kept local to avoid a cross-package dependency). It measures
// the chapter's *content size*, not exact per-run agent context usage — the
// true current context fill lives in the runtime/task layer and needs its own
// data slice. Always present the result as an approximation in the UI.
const CHARS_PER_TOKEN = 3.3;
const CONTEXT_WINDOW_TOKENS = 200_000;

export function estimateSessionTokens(groups: TimelineGroup[]): number {
  let chars = 0;
  for (const g of groups) {
    for (const e of g.entries) {
      if (e.content) chars += e.content.length;
    }
  }
  return Math.round(chars / CHARS_PER_TOKEN);
}

// 0..1 fraction of the 200k window the chapter's content is estimated to fill.
export function estimateSessionContextFraction(groups: TimelineGroup[]): number {
  const f = estimateSessionTokens(groups) / CONTEXT_WINDOW_TOKENS;
  if (f < 0) return 0;
  if (f > 1) return 1;
  return f;
}
