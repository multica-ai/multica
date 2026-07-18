import { describe, expect, it } from "vitest";
import { runNowToastKind, runNowBlockedKey } from "./run-now-toast";

// Elon must-fix 1 (MUL-4525): the "run now" toast must be a whitelist — only
// explicit start statuses are success; every other class (including an unknown
// future status) must NOT show a false "triggered".
describe("runNowToastKind", () => {
  it("shows success only for explicit start statuses", () => {
    expect(runNowToastKind("issue_created")).toBe("success");
    expect(runNowToastKind("running")).toBe("success");
  });

  it("warns on a skipped (admission-blocked) run", () => {
    expect(runNowToastKind("skipped")).toBe("warning");
  });

  it("errors on a failed run", () => {
    expect(runNowToastKind("failed")).toBe("error");
  });

  it("errors on an unknown/future status instead of claiming success", () => {
    expect(runNowToastKind("blocked")).toBe("error");
    expect(runNowToastKind("deferred")).toBe("error");
    expect(runNowToastKind("")).toBe("error");
    expect(runNowToastKind(undefined)).toBe("error");
  });
});

describe("runNowBlockedKey", () => {
  it("maps each known reason_code to its localized message key", () => {
    expect(runNowBlockedKey("invocation_not_allowed")).toBe("run_blocked_invocation_not_allowed");
    expect(runNowBlockedKey("runtime_offline")).toBe("run_blocked_runtime_offline");
    expect(runNowBlockedKey("target_unavailable")).toBe("run_blocked_target_unavailable");
    expect(runNowBlockedKey("attribution_blocked")).toBe("run_blocked_attribution");
    expect(runNowBlockedKey("already_active")).toBe("run_blocked_already_active");
  });

  it("degrades an unknown or absent code to the generic message", () => {
    expect(runNowBlockedKey("some_future_code")).toBe("run_blocked_generic");
    expect(runNowBlockedKey(undefined)).toBe("run_blocked_generic");
  });
});
