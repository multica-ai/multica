// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PluginInvocation } from "@multica/core/types";
import { summarizeInvocations } from "./plugin-hook-activity";

// Canonical matrix for the failure summary. The component suite covers
// rendering; it does not re-run these cases.

function invocation(status: string, error?: string): PluginInvocation {
  return {
    id: Math.random().toString(36).slice(2),
    hook_key: "triage",
    trigger: "event",
    status,
    attempt: 1,
    latency_ms: 5,
    error,
    created_at: "2026-08-19T00:00:00Z",
  };
}

describe("summarizeInvocations", () => {
  it("reports nothing when every recent call succeeded", () => {
    const summary = summarizeInvocations([invocation("ok"), invocation("ok")]);
    expect(summary.failures).toHaveLength(0);
    expect(summary.consecutiveFailures).toBe(0);
  });

  // "Failing right now" and "failed once yesterday" are different stories, and
  // only the first means delivery has stopped.
  it("counts consecutive failures from the newest end only", () => {
    const summary = summarizeInvocations([
      invocation("failed"),
      invocation("failed"),
      invocation("ok"),
      invocation("failed"),
    ]);
    expect(summary.consecutiveFailures).toBe(2);
    expect(summary.failures).toHaveLength(3);
  });

  it("stops counting at a recovery", () => {
    const summary = summarizeInvocations([invocation("ok"), invocation("failed"), invocation("failed")]);
    expect(summary.consecutiveFailures).toBe(0);
  });

  it("surfaces the newest error, which is the one worth showing", () => {
    const summary = summarizeInvocations([
      invocation("failed", "hook endpoint returned 502"),
      invocation("failed", "hook endpoint timed out"),
    ]);
    expect(summary.lastError).toBe("hook endpoint returned 502");
  });

  it("bounds what it reads so an old history cannot dominate", () => {
    const summary = summarizeInvocations(Array.from({ length: 60 }, () => invocation("failed")));
    expect(summary.total).toBe(20);
  });

  it("treats every non-ok status as a failure, not just 'failed'", () => {
    const summary = summarizeInvocations([invocation("timeout"), invocation("refused")]);
    expect(summary.failures).toHaveLength(2);
    expect(summary.consecutiveFailures).toBe(2);
  });
});
