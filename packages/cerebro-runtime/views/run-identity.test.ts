// FIR-4073 — "which agent failed, on which run" is the question the alert row
// has to answer. These tests pin the answer's shape and, more importantly, what
// it does when half the facts are missing.

import { describe, it, expect } from "vitest";
import { formatRunIdentity, formatRunTime } from "./run-identity";

// Mid-day on purpose: a late-evening UTC fixture lands on the next local day in
// any positive-offset timezone, which would make "same day" flap by CI locale.
const now = new Date("2026-07-29T12:00:00Z");

describe("formatRunIdentity", () => {
  it("names the agent first — that is the question being asked", () => {
    const line = formatRunIdentity(
      { agentName: "Sara", runtimeName: "sara-mac" },
      now,
    );
    expect(line.startsWith("Sara")).toBe(true);
    expect(line).toContain("sara-mac");
  });

  it("counts the attempts only when there were several", () => {
    expect(
      formatRunIdentity({ agentName: "Sara", attempt: 2, maxAttempts: 3 }, now),
    ).toContain("attempt 2 of 3");
    expect(
      formatRunIdentity({ agentName: "Sara", attempt: 1, maxAttempts: 1 }, now),
    ).not.toContain("attempt");
  });

  it("drops what it does not know instead of printing placeholders", () => {
    expect(formatRunIdentity({ runtimeName: "sara-mac" }, now)).toBe("sara-mac");
    expect(formatRunIdentity({}, now)).toBe("");
    expect(formatRunIdentity({ agentName: "Sara" }, now)).toBe("Sara");
  });

  it("never leaves a dangling separator when a middle part is missing", () => {
    const line = formatRunIdentity(
      { agentName: "Sara", attempt: 2, maxAttempts: 3 },
      now,
    );
    expect(line).toBe("Sara · attempt 2 of 3");
  });
});

describe("formatRunTime", () => {
  it("shows only the clock for a run that failed today", () => {
    const line = formatRunTime("2026-07-29T10:12:00Z", now);
    expect(line).toMatch(/^\d{1,2}[:.]\d{2}/);
  });

  it("adds the date for an older run, so it cannot read as 'just now'", () => {
    const line = formatRunTime("2026-07-26T20:12:00Z", now);
    expect(line.length).toBeGreaterThan(6);
    expect(line).toMatch(/26/);
  });

  it("degrades instead of crashing on a missing or unparseable timestamp", () => {
    expect(formatRunTime(undefined, now)).toBe("");
    expect(formatRunTime("not-a-date", now)).toBe("");
  });
});
