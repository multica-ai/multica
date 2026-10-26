// @vitest-environment node
import { describe, expect, it } from "vitest";

import { queueConstraintPhrase } from "./queue-wait-copy";
import en from "../locales/en/issues.json";

// A queued run's constraint phrase must say what the recorded value proves —
// nothing more. attempted_unclaimed is the wording trap (#8551): its common
// cause is another claimer winning the row, which is normal scheduling, so
// the label must not read as a fault; and an unknown value from a newer
// server must degrade to the plain queued copy, never leak an enum token.
type Leaf = string | Record<string, unknown>;

// Minimal stand-in for the i18n `t` used by the copy helper: resolves the
// selector against the real en bundle so the test fails if a key is missing.
const t = ((selector: (bundle: unknown) => Leaf) => {
  const value = selector(en as unknown);
  if (typeof value !== "string") {
    throw new Error("selector did not resolve to a string");
  }
  return value;
}) as Parameters<typeof queueConstraintPhrase>[1];

describe("queue wait copy", () => {
  it("maps every recorded constraint to its own phrase", () => {
    expect(queueConstraintPhrase("agent_capacity", t)).toBe(en.inline_run.queue_constraint_agent_capacity);
    expect(queueConstraintPhrase("attempted_unclaimed", t)).toBe(en.inline_run.queue_constraint_attempted_unclaimed);
    expect(queueConstraintPhrase("agent_rebound", t)).toBe(en.inline_run.queue_constraint_agent_rebound);
    expect(queueConstraintPhrase("agent_unbound", t)).toBe(en.inline_run.queue_constraint_agent_unbound);
  });

  it("keeps attempted_unclaimed neutral: a busy queue, not a fault", () => {
    const label = queueConstraintPhrase("attempted_unclaimed", t)!.toLowerCase();
    for (const alarmist of ["fail", "error", "blocked", "could not"]) {
      expect(label).not.toContain(alarmist);
    }
  });

  it("degrades unknown and missing values to the plain queued copy", () => {
    expect(queueConstraintPhrase("some_future_value", t)).toBeUndefined();
    expect(queueConstraintPhrase(undefined, t)).toBeUndefined();
  });
});
