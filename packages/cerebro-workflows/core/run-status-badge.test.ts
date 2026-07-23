import { describe, expect, it } from "vitest";
import { RUN_STATUS_BADGE } from "./types";

// Locks the run-status → badge colour mapping. A failed run must read as an
// error (destructive/red) and an escalated run as needs-attention (warning/
// amber); these were previously inverted.
describe("RUN_STATUS_BADGE", () => {
  it("shows failed as destructive (red) and escalated as warning (amber)", () => {
    expect(RUN_STATUS_BADGE.failed).toContain("text-destructive");
    expect(RUN_STATUS_BADGE.escalated).toContain("text-warning");
  });

  it("maps every run status to a semantic token class (no rogue colours)", () => {
    for (const cls of Object.values(RUN_STATUS_BADGE)) {
      expect(cls).toMatch(/text-(muted-foreground|info|success|destructive|warning)\b/);
    }
  });
});
