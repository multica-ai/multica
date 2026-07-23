import { parseWithFallback } from "@multica/core/api";
import { evalReportSchema } from "./schemas";
import type { EvalReport } from "./types";

// parseRunReport turns an eval run's untyped `results` JSONB into the typed
// per-task report the See-why screen renders. Runs recorded before the real
// engine landed — or by a drifted backend — carry no conforming payload; those
// return null so the UI shows a graceful "no detail" state instead of crashing.
// See CLAUDE.md "API Response Compatibility".
export function parseRunReport(results: Record<string, unknown> | undefined | null): EvalReport | null {
  if (!results || !("cases" in results)) return null;
  return parseWithFallback<EvalReport | null>(results, evalReportSchema, null, { endpoint: "cerebroEvalRunReport" });
}

// passLabel is the plain per-task verdict shown on a case card.
export function passLabel(passed: boolean, error?: string): string {
  if (error) return "Error";
  return passed ? "Pass" : "Fail";
}
