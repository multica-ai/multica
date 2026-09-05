import { describe, expect, it } from "vitest";

import { runFailureBadgeLabel } from "./run-failure-badge";

describe("runFailureBadgeLabel", () => {
  it("names the reasons the backend classifies today", () => {
    expect(runFailureBadgeLabel("agent_error.provider_network")).toBe(
      "Network error",
    );
    expect(runFailureBadgeLabel("skill_bundle_unavailable")).toBe(
      "Skill download failed",
    );
    // #7913: the run died setting up its environment on the executing
    // machine. The badge has to say so — "Failed" alone is what sent the
    // original report's diagnosis at a model provider.
    expect(runFailureBadgeLabel("environment_prepare_failed")).toBe(
      "Environment setup failed",
    );
  });

  it("keeps the pre-MUL-1949 coarse values readable", () => {
    expect(runFailureBadgeLabel("agent_error")).toBe("Agent error");
    expect(runFailureBadgeLabel("manual")).toBe("Manual");
  });

  it("degrades to no badge for a reason newer than this build", () => {
    // The row renders a bare status word instead. Unlike web, a compact badge
    // has no room for the raw wire value, so silence is the fallback — but it
    // must be a miss on THIS map, not a value the parser threw away upstream.
    expect(runFailureBadgeLabel("some_future_reason")).toBeUndefined();
    expect(runFailureBadgeLabel(undefined)).toBeUndefined();
    expect(runFailureBadgeLabel("")).toBeUndefined();
  });
});
