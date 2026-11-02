// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Proposal } from "./proposal-schema";
import { isProposalSnoozed, nextSnoozeEnd, snoozeDateBounds } from "./proposal-snooze";

const at = (iso: string) => ({ snoozed_until: iso }) as Pick<Proposal, "snoozed_until">;

describe("snoozeDateBounds", () => {
  it("runs from tomorrow to thirty days out, in UTC days", () => {
    expect(snoozeDateBounds(new Date("2026-11-02T10:00:00Z"))).toEqual({ min: "2026-11-03", max: "2026-12-02" });
  });

  it("uses the UTC day even when the local day differs", () => {
    // 23:30 UTC on the 2nd is already the 3rd in UTC+2, but the bounds are UTC.
    expect(snoozeDateBounds(new Date("2026-11-02T23:30:00Z"))).toEqual({ min: "2026-11-03", max: "2026-12-02" });
  });

  it("keeps the minimum later than now", () => {
    const now = new Date("2026-11-02T00:00:00Z");
    const { min } = snoozeDateBounds(now);
    expect(Date.parse(`${min}T00:00:00Z`)).toBeGreaterThan(now.getTime());
  });
});

describe("isProposalSnoozed", () => {
  const now = Date.parse("2026-11-02T12:00:00Z");

  it("is false without a snooze", () => {
    expect(isProposalSnoozed(at(null as unknown as string), now)).toBe(false);
  });

  it("is true until the snooze date passes", () => {
    expect(isProposalSnoozed(at("2026-11-05T00:00:00Z"), now)).toBe(true);
    expect(isProposalSnoozed(at("2026-11-02T13:00:00Z"), now)).toBe(true);
  });

  it("is false once the snooze has ended", () => {
    expect(isProposalSnoozed(at("2026-11-02T11:00:00Z"), now)).toBe(false);
    expect(isProposalSnoozed(at("2026-11-02T12:00:00Z"), now)).toBe(false);
  });

  it("treats an unreadable snooze as not snoozed", () => {
    expect(isProposalSnoozed(at("soon"), now)).toBe(false);
  });
});

describe("nextSnoozeEnd", () => {
  const now = Date.parse("2026-11-02T12:00:00Z");

  it("returns the earliest running snooze end", () => {
    const list = [at("2026-11-09T00:00:00Z"), at("2026-11-05T00:00:00Z"), at("2026-11-01T00:00:00Z")];
    expect(nextSnoozeEnd(list, now)).toBe(Date.parse("2026-11-05T00:00:00Z"));
  });

  it("returns null when no snooze is still running", () => {
    expect(nextSnoozeEnd([at("2026-11-01T00:00:00Z")], now)).toBeNull();
    expect(nextSnoozeEnd([], now)).toBeNull();
  });
});
