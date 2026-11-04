// @vitest-environment node
import { describe, expect, it } from "vitest";
import { EMPTY_OFFICE_PULSE, parseOfficePulse, DigestModeSchema } from "./scale";

describe("parseOfficePulse", () => {
  it("reads a pulse and keeps a missing hit rate as null", () => {
    const pulse = parseOfficePulse({ state: "paused", decisions_waiting: 3, oldest_decision_age_seconds: 7200, goals_at_risk: 1 });
    expect(pulse.state).toBe("paused");
    expect(pulse.oldest_decision_age_seconds).toBe(7200);
    expect(pulse.knowledge_retrieval_hit_rate).toBeNull();
    expect(pulse.routines_late).toBe(0);
  });

  it("reads a hit rate of zero as a rate, not as missing", () => {
    expect(parseOfficePulse({ knowledge_retrieval_hit_rate: 0 }).knowledge_retrieval_hit_rate).toBe(0);
  });

  it("degrades a malformed response to the empty pulse", () => {
    expect(parseOfficePulse(undefined)).toEqual(EMPTY_OFFICE_PULSE);
    expect(parseOfficePulse({ decisions_waiting: "many" })).toEqual(EMPTY_OFFICE_PULSE);
  });
});

describe("DigestModeSchema", () => {
  it("reads the setting and the resolved source", () => {
    const mode = DigestModeSchema.parse({ setting: "auto", active_squads: 31, resolved: "floors", auto_floor_threshold: 30 });
    expect(mode.resolved).toBe("floors");
    expect(mode.auto_floor_threshold).toBe(30);
  });
});
